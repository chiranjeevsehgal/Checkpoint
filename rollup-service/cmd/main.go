package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"rollup-service/internal/config"
	"rollup-service/internal/llm"
	"rollup-service/internal/model"
	"rollup-service/internal/storage"
	"rollup-service/internal/summarizer"
)

// rollup-service writes English daily and weekly recap narratives. It polls
// Postgres directly (no Kafka) and runs once per night inside a configurable
// window. Each run refreshes every active user's previous local day; the
// Monday run also backfills the previous week's dailies and writes the weekly.
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
	sum := summarizer.New(client, cfg.Summaries.MaxInputChars)

	if cfg.Processing.WindowEnabled {
		log.Printf("rollup-service summarizing inside window %s-%s (model %s)",
			cfg.Processing.WindowStart, cfg.Processing.WindowEnd, cfg.Groq.Model)
	} else {
		log.Printf("rollup-service continuous mode (model %s)", cfg.Groq.Model)
	}

	runLoop(ctx, store, sum, client, cfg)
	log.Println("shut down cleanly")
}

// runLoop triggers one summary run per night while the window is open.
func runLoop(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, cfg *config.Config) {
	runLoc := cfg.Processing.Loc()
	if runLoc == nil {
		runLoc = cfg.Summaries.Loc()
	}
	ticker := time.NewTicker(cfg.Processing.PollInterval())
	defer ticker.Stop()

	var lastRun string
	for {
		if cfg.Processing.InWindow(time.Now()) {
			if runDay := time.Now().In(runLoc).Format("2006-01-02"); runDay != lastRun {
				if err := runOnce(ctx, store, sum, client, cfg, runLoc, time.Now()); err != nil {
					log.Printf("summary run failed: %v", err)
				}
				lastRun = runDay
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runOnce summarizes every active user on a bounded worker pool.
func runOnce(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, cfg *config.Config, runLoc *time.Location, now time.Time) error {
	since := now.AddDate(0, 0, -(cfg.Summaries.LookbackDays + 2))
	users, err := store.ActiveUsers(ctx, since)
	if err != nil {
		return err
	}
	log.Printf("summarizing %d active user(s)", len(users))

	sem := make(chan struct{}, cfg.Summaries.WorkerConcurrency)
	var wg sync.WaitGroup
	for _, user := range users {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil
		}
		wg.Add(1)
		go func(user model.UserRef) {
			defer wg.Done()
			defer func() { <-sem }()
			summarizeUser(ctx, store, sum, client, cfg, runLoc, user, now)
		}(user)
	}
	wg.Wait()
	return nil
}

// summarizeUser writes the previous local day (plus lookback) and, on Mondays,
// the previous complete week.
func summarizeUser(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, cfg *config.Config, runLoc *time.Location, user model.UserRef, now time.Time) {
	deleting, err := store.IsUserDeleting(ctx, user.UserID)
	if err != nil {
		log.Printf("tombstone check failed user_id=%s: %v", user.UserID, err)
		return
	}
	if deleting {
		return
	}
	loc := userLocation(user, cfg)

	for i := 0; i <= cfg.Summaries.LookbackDays; i++ {
		day := dateOnly(now.In(loc)).AddDate(0, 0, -1-i)
		writeDaily(ctx, store, sum, client, user.UserID, loc, day)
	}

	if now.In(runLoc).Weekday() == time.Monday {
		writeWeekly(ctx, store, sum, client, user.UserID, loc, now)
	}
}

// writeDaily summarizes one local day, skipping days with no transcripts.
func writeDaily(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, userID string, loc *time.Location, day time.Time) {
	src, err := store.DaySources(ctx, userID, loc, day)
	if err != nil {
		log.Printf("day sources failed user_id=%s day=%s: %v", userID, day.Format("2006-01-02"), err)
		return
	}
	if src.IsEmpty() {
		return
	}
	text, err := sum.Daily(ctx, day, src)
	if err != nil {
		log.Printf("daily summary failed user_id=%s day=%s: %v", userID, day.Format("2006-01-02"), err)
		return
	}
	if err := store.UpsertSummary(ctx, model.Summary{
		UserID: userID, Period: model.PeriodDaily, PeriodStart: day, Text: text, Model: client.Model(),
	}); err != nil {
		log.Printf("daily upsert failed user_id=%s day=%s: %v", userID, day.Format("2006-01-02"), err)
	}
}

// writeWeekly backfills any missing dailies for the previous Monday-Sunday week
// and then summarizes the week from them.
func writeWeekly(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, userID string, loc *time.Location, now time.Time) {
	weekStart := mondayOfPreviousWeek(now.In(loc))
	existing, err := store.Summaries(ctx, userID, model.PeriodDaily, weekStart, weekStart.AddDate(0, 0, 6))
	if err != nil {
		log.Printf("weekly lookup failed user_id=%s: %v", userID, err)
		return
	}
	byDay := make(map[string]string, len(existing))
	for _, summary := range existing {
		byDay[summary.PeriodStart.Format("2006-01-02")] = summary.Text
	}

	dailyTexts := make([]string, 7)
	found := false
	for i := range dailyTexts {
		day := weekStart.AddDate(0, 0, i)
		text, ok := byDay[day.Format("2006-01-02")]
		if !ok {
			text = backfillDaily(ctx, store, sum, client, userID, loc, day)
			if text == "" {
				continue
			}
		}
		dailyTexts[i] = text
		found = true
	}
	if !found {
		return
	}

	text, err := sum.Weekly(ctx, weekStart, dailyTexts)
	if err != nil {
		log.Printf("weekly summary failed user_id=%s week=%s: %v", userID, weekStart.Format("2006-01-02"), err)
		return
	}
	if err := store.UpsertSummary(ctx, model.Summary{
		UserID: userID, Period: model.PeriodWeekly, PeriodStart: weekStart, Text: text, Model: client.Model(),
	}); err != nil {
		log.Printf("weekly upsert failed user_id=%s week=%s: %v", userID, weekStart.Format("2006-01-02"), err)
	}
}

// backfillDaily generates and stores a missing daily recap, returning "" when
// the day has no transcripts.
func backfillDaily(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, userID string, loc *time.Location, day time.Time) string {
	src, err := store.DaySources(ctx, userID, loc, day)
	if err != nil {
		log.Printf("backfill sources failed user_id=%s day=%s: %v", userID, day.Format("2006-01-02"), err)
		return ""
	}
	if src.IsEmpty() {
		return ""
	}
	text, err := sum.Daily(ctx, day, src)
	if err != nil {
		log.Printf("backfill daily failed user_id=%s day=%s: %v", userID, day.Format("2006-01-02"), err)
		return ""
	}
	if err := store.UpsertSummary(ctx, model.Summary{
		UserID: userID, Period: model.PeriodDaily, PeriodStart: day, Text: text, Model: client.Model(),
	}); err != nil {
		log.Printf("backfill upsert failed user_id=%s day=%s: %v", userID, day.Format("2006-01-02"), err)
		return ""
	}
	return text
}

// userLocation resolves the user's stored IANA zone, falling back to the
// configured default when it is unset or unreadable.
func userLocation(user model.UserRef, cfg *config.Config) *time.Location {
	if user.Timezone == "" {
		return cfg.Summaries.Loc()
	}
	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		log.Printf("invalid stored timezone %q user_id=%s; using default", user.Timezone, user.UserID)
		return cfg.Summaries.Loc()
	}
	return loc
}

// mondayOfPreviousWeek returns the Monday that starts the last complete
// Monday-Sunday week relative to t.
func mondayOfPreviousWeek(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7 // Monday = 0
	return dateOnly(t).AddDate(0, 0, -offset-7)
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
