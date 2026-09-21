package main

import (
	"context"
	"errors"
	"log/slog"
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
	"extraction-service/internal/observability"
	"extraction-service/internal/storage"
)

func main() {
	slog.SetDefault(observability.New("extraction-service"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	store, err := storage.NewPostgresStore(ctx, cfg.Postgres.DSN())
	if err != nil {
		slog.Error("connecting to postgres", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// Rows stranded in 'processing' by a previous crash go back to pending
	// before new work is accepted.
	if n, err := store.ReclaimStale(ctx, cfg.Batch.ReclaimAfter()); err != nil {
		slog.Error("reclaiming stale jobs", "error", err)
		os.Exit(1)
	} else if n > 0 {
		slog.Info("reclaimed stale processing rows", "count", n)
	}

	consumer := kafka.NewConsumer(cfg.Kafka)
	defer consumer.Close()

	dlq := kafka.NewProducer(cfg.Kafka)
	defer dlq.Close()

	client := llm.New(llm.Options{
		BaseURL:             cfg.Groq.BaseURL,
		APIKey:              cfg.Groq.APIKey(),
		Model:               cfg.Groq.Model,
		MaxCompletionTokens: cfg.Groq.MaxCompletionTokens,
		Temperature:         cfg.Groq.Temperature,
		TopP:                cfg.Groq.TopP,
		ReasoningEffort:     cfg.Groq.ReasoningEffort,
		Timeout:             cfg.Groq.Timeout(),
	})

	slog.Info("listening on kafka topic",
		"topic", cfg.Kafka.ConsumeTopic,
		"group", cfg.Kafka.ConsumerGroup,
		"model", cfg.Groq.Model,
		"batch_size", cfg.Batch.Size,
		"outputs", "todos+reminders+insights")

	health := observability.NewHealthServer(":9082")
	health.Start(ctx)
	health.SetReady(true)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		runConsumer(ctx, consumer, store, dlq, cfg, health)
	}()
	go func() {
		defer wg.Done()
		runReclaimer(ctx, store, cfg)
	}()
	go func() {
		defer wg.Done()
		runBatcher(ctx, store, client, cfg, health)
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	wg.Wait()
	slog.Info("shut down cleanly")
}

// runConsumer is the fast path: event → pending queue row → commit. No LLM
// work happens here — that is entirely the batcher's job. A poison message
// goes to the DLQ and is committed so it never comes back.
func runConsumer(ctx context.Context, consumer *kafka.Consumer, store *storage.PostgresStore, dlq *kafka.Producer, cfg *config.Config, health *observability.HealthServer) {
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
					slog.Error("dlq send failed (will retry)", "error", dlqErr)
					continue // not committed: redelivered, DLQ retried
				}
				if err := consumer.Commit(ctx, msg); err != nil {
					slog.Error("commit error for poison message", "error", err)
				}
				continue
			}
			slog.Error("read error", "error", err)
			continue
		}

		if err := event.Validate(); err != nil {
			if dlqErr := dlq.SendToDLQ(ctx, cfg.Kafka.ConsumeTopic, "INVALID_EVENT", err.Error(), msg.Value); dlqErr != nil {
				slog.Error("dlq send failed (will retry)", "error", dlqErr)
				continue
			}
			if err := consumer.Commit(ctx, msg); err != nil {
				slog.Error("commit error for invalid event", "error", err)
			}
			continue
		}

		deleting, err := store.IsUserDeleting(ctx, event.Data.UserID)
		if err != nil {
			// Not committing means redelivery on restart — the event is
			// never silently lost.
			slog.Error("tombstone check failed", "audio_id", event.Data.AudioID, "error", err)
			continue
		}
		if deleting {
			slog.Info("stale_deleted_user_event", "audio_id", event.Data.AudioID)
			health.Inc("extraction_consumer_stale_total")
			if err := consumer.Commit(ctx, msg); err != nil {
				slog.Error("commit error for stale event", "error", err)
			}
			continue
		}

		// One combined queue row per audio; the ON CONFLICT makes Kafka
		// redelivery a no-op.
		job := model.Job{
			UserID:         event.Data.UserID,
			AudioID:        event.Data.AudioID,
			ExtractionType: model.TypeAll,
			Text:           event.Data.Text,
			Language:       event.Data.Language,
			RecordedAt:     event.Data.RecordedAt,
		}
		if err := store.EnqueueJob(ctx, job); err != nil {
			// Not committing means redelivery on restart — the event is
			// never silently lost.
			slog.Error("enqueue failed", "audio_id", event.Data.AudioID, "error", err)
			health.Inc("extraction_consumer_failed_total")
			continue
		}
		health.Inc("extraction_consumer_enqueued_total")

		if err := consumer.Commit(ctx, msg); err != nil {
			slog.Error("commit error", "audio_id", event.Data.AudioID, "error", err)
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
				slog.Error("reclaim error", "error", err)
			} else if n > 0 {
				slog.Info("reclaimed stale processing rows", "count", n)
			}
		}
	}
}

// runBatcher polls the queue for users with a full batch and runs each
// batch on a bounded worker pool. One batch = one user; batches for different
// users run in parallel, never mixed.
func runBatcher(ctx context.Context, store *storage.PostgresStore, client *llm.Client, cfg *config.Config, health *observability.HealthServer) {
	sem := make(chan struct{}, cfg.Batch.WorkerConcurrency)
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-time.After(cfg.Batch.PollInterval()):
		}

		users, err := store.ReadyUsers(ctx, model.TypeAll, cfg.Batch.Size, cfg.Batch.MaxWait())
		if err != nil {
			slog.Error("ready users error", "error", err)
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
			go func(userID string) {
				defer wg.Done()
				defer func() { <-sem }()
				processBatch(ctx, store, client, userID, cfg, health)
			}(u)
		}
	}
}

// processBatch claims one user's rows and extracts them. A claimed batch
// larger than max_batch_chars is split into multiple LLM calls; all groups
// must succeed before their jobs are marked done.
func processBatch(ctx context.Context, store *storage.PostgresStore, client *llm.Client, userID string, cfg *config.Config, health *observability.HealthServer) {
	jobs, err := store.ClaimBatch(ctx, userID, model.TypeAll, cfg.Batch.Size)
	if err != nil {
		slog.Error("claim error", "user_id", userID, "error", err)
		health.Inc("extraction_batches_failed_total")
		return
	}
	if len(jobs) == 0 {
		return
	}
	ext := extractor.New(userLocation(ctx, store, userID, cfg))
	modelTag := client.Model() + "@" + extractor.PromptVersion

	for _, group := range splitByChars(jobs, cfg.Batch.MaxBatchChars) {
		content, err := client.Chat(ctx, ext.Messages(group))
		if err != nil {
			handleBatchFailure(ctx, store, group, err, cfg)
			health.Inc("extraction_batches_failed_total")
			return
		}
		results, err := ext.Parse(content, group)
		if err != nil {
			handleBatchFailure(ctx, store, group, err, cfg)
			health.Inc("extraction_batches_failed_total")
			return
		}
		if err := store.CompleteBatch(ctx, results, modelTag); err != nil {
			handleBatchFailure(ctx, store, group, err, cfg)
			health.Inc("extraction_batches_failed_total")
			return
		}
		health.Inc("extraction_batches_ok_total")
		skipped := 0
		entries := 0
		for _, r := range results {
			if r.IsEmpty() {
				skipped++
			}
			entries += len(r.Todos) + len(r.Reminders) + len(r.Insights)
		}
		slog.Info("extracted",
			"user_id", userID,
			"items", len(group),
			"done", len(results)-skipped,
			"skipped", skipped,
			"entries", entries)
	}
}

// userLocation resolves the user's stored IANA zone, falling back to the
// configured default when the user has none or it is unreadable.
func userLocation(ctx context.Context, store *storage.PostgresStore, userID string, cfg *config.Config) *time.Location {
	timezone, err := store.UserTimezone(ctx, userID)
	if err != nil {
		slog.Error("timezone lookup failed", "user_id", userID, "error", err)
		return cfg.Reminders.Loc()
	}
	if timezone == "" {
		return cfg.Reminders.Loc()
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		slog.Warn("invalid stored timezone; using default", "timezone", timezone, "user_id", userID)
		return cfg.Reminders.Loc()
	}
	return loc
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
			slog.Error("release error", "error", rErr)
		}
	}
	if len(fail) > 0 {
		if fErr := store.FailJobs(ctx, fail, err.Error()); fErr != nil {
			slog.Error("fail error", "error", fErr)
		}
	}
	slog.Error("batch failed", "released", len(release), "failed", len(fail), "error", err)

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
