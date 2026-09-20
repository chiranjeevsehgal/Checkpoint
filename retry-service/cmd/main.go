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

	"retry-service/internal/config"
	"retry-service/internal/kafka"
	"retry-service/internal/model"
	"retry-service/internal/storage"
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
	if n, err := store.ReclaimStale(ctx, cfg.Retry.ReclaimAfter()); err != nil {
		log.Fatalf("reclaiming stale jobs: %v", err)
	} else if n > 0 {
		log.Printf("reclaimed %d stale processing rows", n)
	}

	consumer := kafka.NewConsumer(cfg.Kafka)
	defer consumer.Close()

	producer := kafka.NewProducer(cfg.Kafka)
	defer producer.Close()

	log.Printf("listening on kafka topic %q (group %q), delay=%s max_attempts=%d, re-delivering originals to their source topics",
		cfg.Kafka.ConsumeTopic, cfg.Kafka.ConsumerGroup, cfg.Retry.Delay(), cfg.Retry.MaxAttempts)

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		runConsumer(ctx, consumer, store, producer, cfg)
	}()
	go func() {
		defer wg.Done()
		runDispatcher(ctx, store, producer, cfg)
	}()
	go func() {
		defer wg.Done()
		runReclaimer(ctx, store, cfg)
	}()
	go func() {
		defer wg.Done()
		runPruner(ctx, store, cfg)
	}()

	<-ctx.Done()
	log.Println("shutting down")
	wg.Wait()
	log.Println("shut down cleanly")
}

// runConsumer folds handoff events into the durable queue. Every path ends
// in a commit except a Postgres failure (redelivery on restart) and a DLQ
// publish failure (the poison handoff is retried); RecordFailure is
// idempotent, so redelivered handoffs are folded safely.
func runConsumer(ctx context.Context, consumer *kafka.Consumer, store *storage.PostgresStore, producer *kafka.Producer, cfg *config.Config) {
	for {
		var event model.RetryRequestedEvent
		msg, err := consumer.ReadMessage(ctx, &event)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var malformed *kafka.MalformedMessageError
			if errors.As(err, &malformed) {
				if dlqErr := producer.SendToDLQ(ctx, cfg.Kafka.ConsumeTopic, "INVALID_EVENT", err.Error(), msg.Value); dlqErr != nil {
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

		parsed, err := event.Validate()
		if err != nil {
			if dlqErr := producer.SendToDLQ(ctx, cfg.Kafka.ConsumeTopic, "INVALID_EVENT", err.Error(), msg.Value); dlqErr != nil {
				log.Printf("dlq send failed (will retry): %v", dlqErr)
				continue
			}
			if err := consumer.Commit(ctx, msg); err != nil {
				log.Printf("commit error for invalid event: %v", err)
			}
			continue
		}

		attemptedAt := event.OccurredAt
		if attemptedAt == "" {
			attemptedAt = time.Now().UTC().Format(time.RFC3339)
		}
		rec := model.FailureRecord{
			OriginalEventID: parsed.EventID,
			SourceService:   event.Data.SourceService,
			SourceTopic:     event.Data.SourceTopic,
			MessageKey:      string(msg.Key),
			UserID:          parsed.UserID,
			OriginalPayload: event.Data.OriginalEvent,
			Entry: model.AttemptEntry{
				AttemptedAt:  attemptedAt,
				Stage:        event.Data.Stage,
				ErrorCode:    event.Data.ErrorCode,
				ErrorMessage: event.Data.ErrorMessage,
			},
		}

		outcome, attempts, err := store.RecordFailure(ctx, rec, cfg.Retry.MaxAttempts, cfg.Retry.Delay())
		if err != nil {
			// Not committing means redelivery on restart — the handoff
			// is never silently lost, and folding it twice is safe.
			log.Printf("record failure failed original_event_id=%s: %v", parsed.EventID, err)
			continue
		}

		switch outcome {
		case model.OutcomeScheduled:
			log.Printf("retry scheduled original_event_id=%s source=%s topic=%s attempt_due_in=%s stage=%s",
				parsed.EventID, event.Data.SourceService, event.Data.SourceTopic, cfg.Retry.Delay(), event.Data.Stage)
		case model.OutcomeRearmed:
			log.Printf("retry re-armed original_event_id=%s source=%s attempts=%d/%d stage=%s",
				parsed.EventID, event.Data.SourceService, attempts+1, cfg.Retry.MaxAttempts, event.Data.Stage)
		case model.OutcomeFailed:
			log.Printf("retry exhausted original_event_id=%s source=%s topic=%s attempts=%d stage=%s error=%s",
				parsed.EventID, event.Data.SourceService, event.Data.SourceTopic, attempts, event.Data.Stage, event.Data.ErrorMessage)
		case model.OutcomeDuplicate:
			log.Printf("duplicate handoff original_event_id=%s source=%s stage=%s (schedule stands)",
				parsed.EventID, event.Data.SourceService, event.Data.Stage)
		case model.OutcomeTerminal:
			log.Printf("late handoff for terminal job original_event_id=%s source=%s (ignored)",
				parsed.EventID, event.Data.SourceService)
		}

		if err := consumer.Commit(ctx, msg); err != nil {
			log.Printf("commit error for original_event_id=%s: %v", parsed.EventID, err)
		}
	}
}

// runDispatcher polls the queue for due retries and re-publishes each
// original payload to its source topic. Broker failures release the claim
// with the schedule untouched — the next poll retries, indefinitely, and
// never consumes a retry attempt.
func runDispatcher(ctx context.Context, store *storage.PostgresStore, producer *kafka.Producer, cfg *config.Config) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.Retry.PollInterval()):
		}

		jobs, err := store.ClaimDue(ctx, cfg.Retry.BatchSize)
		if err != nil {
			log.Printf("claim error: %v", err)
			continue
		}
		for _, job := range jobs {
			dispatchJob(ctx, store, producer, job)
		}
	}
}

func dispatchJob(ctx context.Context, store *storage.PostgresStore, producer *kafka.Producer, job model.RetryJob) {
	// The account may have been deleted while the retry waited; never
	// re-deliver a tombstoned user's events to the workers.
	if job.UserID != "" {
		deleting, err := store.IsUserDeleting(ctx, job.UserID)
		if err != nil {
			log.Printf("tombstone check failed original_event_id=%s: %v", job.OriginalEventID, err)
			releaseQuietly(ctx, store, job)
			return
		}
		if deleting {
			log.Printf("skipping retry for deleted account original_event_id=%s", job.OriginalEventID)
			if err := store.MarkSkipped(ctx, job.ID); err != nil {
				log.Printf("mark skipped failed original_event_id=%s: %v", job.OriginalEventID, err)
				releaseQuietly(ctx, store, job)
			}
			return
		}
	}

	if err := producer.Publish(ctx, job.SourceTopic, job.MessageKey, job.OriginalPayload); err != nil {
		log.Printf("re-delivery failed original_event_id=%s topic=%s: %v", job.OriginalEventID, job.SourceTopic, err)
		releaseQuietly(ctx, store, job)
		return
	}

	if err := store.MarkDispatched(ctx, job.ID); err != nil {
		// The payload is already back on the source topic; the row stays
		// 'processing' and is reclaimed after the stale window, which may
		// re-deliver it once more. That is at-least-once, like everything
		// else on the bus.
		log.Printf("mark dispatched failed original_event_id=%s: %v", job.OriginalEventID, err)
		return
	}
	log.Printf("re-delivered original_event_id=%s to topic=%s (source=%s attempt=%d)",
		job.OriginalEventID, job.SourceTopic, job.SourceService, job.Attempts+1)
}

func releaseQuietly(ctx context.Context, store *storage.PostgresStore, job model.RetryJob) {
	if err := store.ReleaseClaim(ctx, job.ID); err != nil {
		log.Printf("release failed original_event_id=%s: %v", job.OriginalEventID, err)
	}
}

// runReclaimer periodically resets rows stuck in 'processing' (crashed
// workers). The threshold must exceed the longest publish so live claims
// are never disturbed.
func runReclaimer(ctx context.Context, store *storage.PostgresStore, cfg *config.Config) {
	interval := cfg.Retry.ReclaimAfter()
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
			if n, err := store.ReclaimStale(ctx, cfg.Retry.ReclaimAfter()); err != nil {
				log.Printf("reclaim error: %v", err)
			} else if n > 0 {
				log.Printf("reclaimed %d stale processing rows", n)
			}
		}
	}
}

// runPruner drops terminal rows past the retention window so the table
// stays inspectable without growing forever.
func runPruner(ctx context.Context, store *storage.PostgresStore, cfg *config.Config) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := store.PruneTerminal(ctx, cfg.Retry.Retention()); err != nil {
				log.Printf("prune error: %v", err)
			} else if n > 0 {
				log.Printf("pruned %d terminal rows past retention", n)
			}
		}
	}
}
