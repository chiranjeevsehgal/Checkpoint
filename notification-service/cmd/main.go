package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"notification-service/internal/config"
	"notification-service/internal/model"
	"notification-service/internal/ntfy"
	"notification-service/internal/observability"
	"notification-service/internal/storage"
)

// notification-service turns due reminders into ntfy push notifications. It
// polls Postgres directly (no Kafka): advance fires at remind_at minus the
// configured lead, due fires at remind_at.
func main() {
	slog.SetDefault(observability.New("notification-service"))

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

	client := ntfy.New(ntfy.Options{
		BaseURL:      cfg.Ntfy.URL,
		Token:        cfg.Ntfy.Token(),
		Timeout:      cfg.Ntfy.Timeout(),
		MaxBodyBytes: cfg.Ntfy.MaxMessageBytes,
	})

	slog.Info("notifying reminders",
		"url", cfg.Ntfy.URL,
		"advance", cfg.Ntfy.Advance(),
		"poll_interval", cfg.Delivery.PollInterval())

	runLoop(ctx, store, client, cfg)
	slog.Info("shut down cleanly")
}

// runLoop polls for reminders whose advance or due fire time has arrived and
// publishes them on a bounded worker pool.
func runLoop(ctx context.Context, store *storage.PostgresStore, client *ntfy.Client, cfg *config.Config) {
	ticker := time.NewTicker(cfg.Delivery.PollInterval())
	defer ticker.Stop()

	lastPrune := time.Time{}
	for {
		processDue(ctx, store, client, cfg)
		if time.Since(lastPrune) >= time.Hour {
			if n, err := store.PruneOld(ctx, cfg.Delivery.Retention()); err != nil {
				slog.Error("prune error", "error", err)
			} else if n > 0 {
				slog.Info("pruned old delivery rows", "count", n)
			}
			lastPrune = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// processDue drains both delivery kinds each poll.
func processDue(ctx context.Context, store *storage.PostgresStore, client *ntfy.Client, cfg *config.Config) {
	var candidates []model.Candidate
	for _, kind := range []string{model.KindAdvance, model.KindDue} {
		list, err := store.DueCandidates(ctx, kind,
			cfg.Ntfy.Advance(), cfg.Delivery.AdvanceGrace(), cfg.Delivery.MaxLateness(), cfg.Ntfy.AdvanceMax(), cfg.Delivery.BatchSize)
		if err != nil {
			slog.Error("candidate query failed", "kind", kind, "error", err)
			continue
		}
		candidates = append(candidates, list...)
	}
	if len(candidates) == 0 {
		return
	}

	sem := make(chan struct{}, cfg.Delivery.WorkerConcurrency)
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(c model.Candidate) {
			defer wg.Done()
			defer func() { <-sem }()
			deliver(ctx, store, client, cfg, c)
		}(candidate)
	}
	wg.Wait()
}

// deliver claims one delivery, publishes it, and records the outcome. A
// retryable failure leaves the row failed so the next poll re-claims it.
func deliver(ctx context.Context, store *storage.PostgresStore, client *ntfy.Client, cfg *config.Config, c model.Candidate) {
	id, attempts, claimed, err := store.ClaimDelivery(ctx, c, cfg.Delivery.MaxAttempts, cfg.Delivery.ReclaimAfter())
	if err != nil {
		slog.Error("claim failed", "user_id", c.UserID, "error", err)
		return
	}
	if !claimed {
		return
	}

	title, body := message(c, cfg.Ntfy.Advance())
	priority := cfg.Ntfy.AdvancePriority
	if c.Kind == model.KindDue {
		priority = cfg.Ntfy.DuePriority
	}

	pubCtx, cancel := context.WithTimeout(ctx, cfg.Ntfy.Timeout())
	defer cancel()
	if err := client.Publish(pubCtx, c.Topic, title, body, priority); err != nil {
		if ferr := store.MarkFailed(ctx, id, err.Error()); ferr != nil {
			slog.Error("mark failed", "id", id, "error", ferr)
		}
		slog.Error("delivery failed", "kind", c.Kind, "user_id", c.UserID, "attempt", attempts, "error", err)
		return
	}
	if err := store.MarkSent(ctx, id); err != nil {
		slog.Error("mark sent failed", "id", id, "error", err)
		return
	}
	slog.Info("reminder notified", "kind", c.Kind, "user_id", c.UserID, "attempts", attempts)
}

// message renders the advance and due notification wording. The advance uses
// the lead the delivery was scheduled with, falling back to the configured
// default when it is not set.
func message(c model.Candidate, defaultAdvance time.Duration) (string, string) {
	if c.Kind == model.KindAdvance {
		minutes := int(defaultAdvance.Minutes())
		if c.AdvanceSeconds > 0 {
			minutes = c.AdvanceSeconds / 60
		}
		return "Upcoming reminder", fmt.Sprintf("%s in %d minutes", c.ReminderText, minutes)
	}
	return "Reminder", c.ReminderText
}
