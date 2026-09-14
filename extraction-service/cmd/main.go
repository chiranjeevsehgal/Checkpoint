package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"extraction-service/internal/config"
	"extraction-service/internal/extractor"
	"extraction-service/internal/kafka"
	"extraction-service/internal/llm"
	"extraction-service/internal/model"
	"extraction-service/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	store, err := storage.NewPostgresStore(ctx, cfg.Postgres.DSN())
	if err != nil {
		log.Fatalf("connecting to postgres: %v", err)
	}
	defer store.Close()

	// Rows stranded in 'processing' by a previous crash go back to pending
	// before new work is accepted.
	if n, err := store.ReclaimStale(ctx, cfg.Batch.ReclaimAfter()); err != nil {
		log.Fatalf("reclaiming stale jobs: %v", err)
	} else if n > 0 {
		log.Printf("reclaimed %d stale processing rows", n)
	}

	consumer := kafka.NewConsumer(cfg.Kafka)
	defer consumer.Close()

	dlq := kafka.NewProducer(cfg.Kafka)
	defer dlq.Close()

	client := llm.New(cfg.Groq.BaseURL, cfg.Groq.APIKey(), cfg.Groq.Model, cfg.Groq.MaxCompletionTokens, cfg.Groq.Temperature, cfg.Groq.Timeout())

	registry := extractor.NewRegistry()
	registry.Register(extractor.TodoExtractor{})

	log.Printf("listening on kafka topic %q (group %q), model %s, batch=%d, writing todos to postgres",
		cfg.Kafka.ConsumeTopic, cfg.Kafka.ConsumerGroup, cfg.Groq.Model, cfg.Batch.Size)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		runConsumer(ctx, consumer, store, dlq, cfg, registry)
	}()
	go func() {
		defer wg.Done()
		runReclaimer(ctx, store, cfg)
	}()
	go func() {
		defer wg.Done()
		runBatcher(ctx, store, client, cfg, registry)
	}()

	<-ctx.Done()
	log.Println("shutting down")
	wg.Wait()
	log.Println("shut down cleanly")
}

// runConsumer is the fast path: event → pending queue row → commit. No LLM
// work happens here — that is entirely the batcher's job. A poison message
// goes to the DLQ and is committed so it never comes back.
func runConsumer(ctx context.Context, consumer *kafka.Consumer, store *storage.PostgresStore, dlq *kafka.Producer, cfg *config.Config, registry *extractor.Registry) {
	for {
		var event model.ExtractionJobRequestedEvent
		msg, err := consumer.ReadMessage(ctx, &event)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var malformed *kafka.MalformedMessageError
			if errors.As(err, &malformed) {
				if dlqErr := dlq.SendToDLQ(ctx, cfg.Kafka.ConsumeTopic, "INVALID_EVENT", err.Error(), msg.Value); dlqErr != nil {
					log.Printf("dlq send failed (will retry): %v", dlqErr)
					continue // not committed: redelivered, DLQ retried
				}
				if err := consumer.Commit(ctx, msg); err != nil {
					log.Printf("commit error for poison message: %v", err)
				}
				continue
			}
			log.Printf("read error: %v", err)
			continue
		}

		if err := event.Validate(); err != nil {
			if dlqErr := dlq.SendToDLQ(ctx, cfg.Kafka.ConsumeTopic, "INVALID_EVENT", err.Error(), msg.Value); dlqErr != nil {
				log.Printf("dlq send failed (will retry): %v", dlqErr)
				continue
			}
			if err := consumer.Commit(ctx, msg); err != nil {
				log.Printf("commit error for invalid event: %v", err)
			}
			continue
		}

		// One queue row per registered extraction type; today that is just
		// "todo". ON CONFLICT DO NOTHING makes Kafka redelivery a no-op.
		var enqueueErr error
		for _, typ := range registry.Types() {
			job := model.Job{
				UserID:          event.Data.UserID,
				AudioID:         event.Data.AudioID,
				ExtractionType:  typ,
				Text:            event.Data.Text,
				Language:        event.Data.Language,
			}
			if err := store.EnqueueJob(ctx, job); err != nil {
				enqueueErr = err
				break
			}
		}
		if enqueueErr != nil {
			// Not committing means redelivery on restart — the event is
			// never silently lost.
			log.Printf("enqueue failed audio_id=%s: %v", event.Data.AudioID, enqueueErr)
			continue
		}

		if err := consumer.Commit(ctx, msg); err != nil {
			log.Printf("commit error for audio_id=%s: %v", event.Data.AudioID, err)
		}
	}
}

// runReclaimer periodically resets rows stuck in 'processing' (crashed
// workers). The threshold must exceed the longest LLM call so live batches
// are never disturbed.
func runReclaimer(ctx context.Context, store *storage.PostgresStore, cfg *config.Config) {
	interval := cfg.Batch.ReclaimAfter()
	if interval < time.Minute {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := store.ReclaimStale(ctx, cfg.Batch.ReclaimAfter()); err != nil {
				log.Printf("reclaim error: %v", err)
			} else if n > 0 {
				log.Printf("reclaimed %d stale processing rows", n)
			}
		}
	}
}

// runBatcher polls the queue for users with a full batch and runs each
// batch on a bounded worker pool. One batch = one user = one type; batches
// for different users run in parallel, never mixed.
func runBatcher(ctx context.Context, store *storage.PostgresStore, client *llm.Client, cfg *config.Config, registry *extractor.Registry) {
	sem := make(chan struct{}, cfg.Batch.WorkerConcurrency)
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-time.After(cfg.Batch.PollInterval()):
		}

		for _, typ := range registry.Types() {
			ext, ok := registry.Get(typ)
			if !ok {
				continue
			}
			users, err := store.ReadyUsers(ctx, typ, cfg.Batch.Size, cfg.Batch.MaxWait())
			if err != nil {
				log.Printf("ready users error: %v", err)
				continue
			}
			for _, u := range users {
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					wg.Wait()
					return
				}
				wg.Add(1)
				go func(userID string, ext extractor.Extractor) {
					defer wg.Done()
					defer func() { <-sem }()
					processBatch(ctx, store, client, ext, userID, cfg)
				}(u, ext)
			}
		}
	}
}

// processBatch claims one user's rows and extracts them. A claimed batch
// larger than max_batch_chars is split into multiple LLM calls; all groups
// must succeed before their jobs are marked done.
func processBatch(ctx context.Context, store *storage.PostgresStore, client *llm.Client, ext extractor.Extractor, userID string, cfg *config.Config) {
	jobs, err := store.ClaimBatch(ctx, userID, ext.Type(), cfg.Batch.Size)
	if err != nil {
		log.Printf("claim error user_id=%s type=%s: %v", userID, ext.Type(), err)
		return
	}
	if len(jobs) == 0 {
		return
	}

	for _, group := range splitByChars(jobs, cfg.Batch.MaxBatchChars) {
		content, err := client.Chat(ctx, ext.Messages(group))
		if err != nil {
			handleBatchFailure(ctx, store, group, err, cfg)
			return
		}
		results, err := ext.Parse(content, group)
		if err != nil {
			handleBatchFailure(ctx, store, group, err, cfg)
			return
		}
		if err := store.CompleteBatch(ctx, results, client.Model()); err != nil {
			handleBatchFailure(ctx, store, group, err, cfg)
			return
		}
		todos := 0
		for _, r := range results {
			todos += len(r.Todos)
		}
		log.Printf("extracted user_id=%s type=%s items=%d todos=%d", userID, ext.Type(), len(group), todos)
	}
}

// handleBatchFailure splits the claimed rows: rows that still have attempts
// left go back to pending (the next poll re-claims them); rows that hit
// max_attempts are marked failed with the error for manual requeue.
func handleBatchFailure(ctx context.Context, store *storage.PostgresStore, jobs []model.Job, err error, cfg *config.Config) {
	var release, fail []int64
	for _, j := range jobs {
		if j.Attempts >= cfg.Batch.MaxAttempts {
			fail = append(fail, j.ID)
		} else {
			release = append(release, j.ID)
		}
	}
	if len(release) > 0 {
		if rErr := store.ReleaseJobs(ctx, release, err.Error()); rErr != nil {
			log.Printf("release error: %v", rErr)
		}
	}
	if len(fail) > 0 {
		if fErr := store.FailJobs(ctx, fail, err.Error()); fErr != nil {
			log.Printf("fail error: %v", fErr)
		}
	}
	log.Printf("batch failed (released=%d failed=%d): %v", len(release), len(fail), err)

	// Small backoff so a downed Groq isn't hammered at poll speed. The
	// sleep is capped and cancellable.
	backoff := time.Duration(1<<min(jobs[0].Attempts, 5)) * time.Second // 2,4,...,32s
	if backoff > 30*time.Second {
		backoff = 30 * time.Second
	}
	select {
	case <-ctx.Done():
	case <-time.After(backoff):
	}
}

// splitByChars packs jobs into groups whose cumulative transcript length
// stays under maxChars. An oversized single job becomes its own group —
// Groq may reject it, which counts as a retryable failure like any other.
func splitByChars(jobs []model.Job, maxChars int) [][]model.Job {
	if len(jobs) == 0 {
		return nil
	}
	var groups [][]model.Job
	var current []model.Job
	size := 0
	for _, j := range jobs {
		n := len(j.Text)
		if len(current) > 0 && size+n > maxChars {
			groups = append(groups, current)
			current, size = nil, 0
		}
		current = append(current, j)
		size += n
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}
