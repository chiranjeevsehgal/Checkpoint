package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"rollup-service/internal/config"
	"rollup-service/internal/llm"
	"rollup-service/internal/model"
	"rollup-service/internal/observability"
	"rollup-service/internal/storage"
	"rollup-service/internal/summarizer"
)

// mondayLookbackDays covers the previous Mon-Sun week across all timezones, so
// Monday's weekly run enumerates every user active during that week.
const mondayLookbackDays = 9

// runStats aggregates one run's outcomes for a single log line.
type runStats struct {
	users  int
	daily  atomic.Int64
	weekly atomic.Int64
	failed atomic.Int64
}

// rollup-service writes English daily and weekly recap narratives. It polls
// Postgres directly (no Kafka) and runs once per night inside a configurable
// window. Each run refreshes every active user's previous local day; the
// Monday run also backfills the previous week's dailies and writes the weekly.
func main() {
	slog.SetDefault(observability.New("rollup-service"))

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

	client := llm.New(llm.Options{
		BaseURL:             cfg.Groq.BaseURL,
		APIKey:              cfg.Groq.APIKey(),
		Model:               cfg.Groq.Model,
		MaxCompletionTokens: cfg.Groq.MaxCompletionTokens,
		Temperature:         cfg.Groq.Temperature,
		TopP:                cfg.Groq.TopP,
		ReasoningEffort:     cfg.Groq.ReasoningEffort,
		Timeout:             cfg.Groq.Timeout(),
		MaxAttempts:         cfg.Groq.MaxAttempts,
	})
	sum := summarizer.New(client, cfg.Summaries.MaxInputChars)

	if cfg.Processing.WindowEnabled {
		slog.Info("summarizing inside window",
			"window_start", cfg.Processing.WindowStart,
			"window_end", cfg.Processing.WindowEnd,
			"model", cfg.Groq.Model)
	} else {
		slog.Info("continuous mode", "model", cfg.Groq.Model)
	}

	runLoop(ctx, store, sum, client, cfg)
	slog.Info("shut down cleanly")
}

// runLoop triggers one summary run per night while the window is open. The
// nightly gate advances only on success, so a failed run is retried with
// backoff until the window closes.
func runLoop(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, cfg *config.Config) {
	runLoc := cfg.Processing.Loc()
	if runLoc == nil {
		runLoc = cfg.Summaries.Loc()
	}
	ticker := time.NewTicker(cfg.Processing.PollInterval())
	defer ticker.Stop()

	var lastRun string
	var attempt int
	var nextAttempt time.Time
	for {
		now := time.Now()
		if cfg.Processing.InWindow(now) && !now.Before(nextAttempt) {
			if runDay := now.In(runLoc).Format("2006-01-02"); runDay != lastRun {
				stats, err := runOnce(ctx, store, sum, client, cfg, runLoc, now)
				if err != nil {
					attempt++
					nextAttempt = now.Add(runRetryDelay(attempt))
					slog.Error("summary run failed", "attempt", attempt, "error", err)
				} else {
					slog.Info("summary run complete",
						"users", stats.users,
						"daily", stats.daily.Load(),
						"weekly", stats.weekly.Load(),
						"failed", stats.failed.Load())
					lastRun = runDay
					attempt = 0
					nextAttempt = time.Time{}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runRetryDelay bounds how often a failed nightly run is retried.
func runRetryDelay(attempt int) time.Duration {
	delay := time.Duration(1<<(attempt-1)) * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}

// activitySince is the transcript look-back floor: the normal daily window,
// widened on Monday so the weekly run sees the whole previous local week.
func activitySince(now time.Time, runLoc *time.Location, lookbackDays int) time.Time {
	days := lookbackDays + 2
	if now.In(runLoc).Weekday() == time.Monday && mondayLookbackDays > days {
		days = mondayLookbackDays
	}
	return now.AddDate(0, 0, -days)
}

// runOnce summarizes every active user on a bounded worker pool. It returns an
// error only for enumeration failures; per-user failures are counted in stats.
func runOnce(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, cfg *config.Config, runLoc *time.Location, now time.Time) (*runStats, error) {
	since := activitySince(now, runLoc, cfg.Summaries.LookbackDays)
	users, err := store.ActiveUsers(ctx, since)
	if err != nil {
		return nil, err
	}
	stats := &runStats{users: len(users)}
	slog.Info("summarizing active users", "users", len(users))

	sem := make(chan struct{}, cfg.Summaries.WorkerConcurrency)
	var wg sync.WaitGroup
	for _, user := range users {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return stats, nil
		}
		wg.Add(1)
		go func(user model.UserRef) {
			defer wg.Done()
			defer func() { <-sem }()
			summarizeUser(ctx, store, sum, client, cfg, runLoc, user, now, stats)
		}(user)
	}
	wg.Wait()
	return stats, nil
}

// summarizeUser writes the previous local day (plus lookback) and, on Mondays,
// the previous complete week.
func summarizeUser(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, cfg *config.Config, runLoc *time.Location, user model.UserRef, now time.Time, stats *runStats) {
	deleting, err := store.IsUserDeleting(ctx, user.UserID)
	if err != nil {
		slog.Error("tombstone check failed", "user_id", user.UserID, "error", err)
		stats.failed.Add(1)
		return
	}
	if deleting {
		return
	}
	loc := userLocation(user, cfg)

	for i := 0; i <= cfg.Summaries.LookbackDays; i++ {
		day := dateOnly(now.In(loc)).AddDate(0, 0, -1-i)
		writeDaily(ctx, store, sum, client, user.UserID, loc, day, stats)
	}

	if now.In(runLoc).Weekday() == time.Monday {
		writeWeekly(ctx, store, sum, client, user.UserID, loc, now, stats)
	}
}

// writeDaily summarizes one local day, skipping days with no transcripts.
func writeDaily(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, userID string, loc *time.Location, day time.Time, stats *runStats) {
	src, err := store.DaySources(ctx, userID, loc, day)
	if err != nil {
		slog.Error("day sources failed", "user_id", userID, "day", day.Format("2006-01-02"), "error", err)
		stats.failed.Add(1)
		return
	}
	if src.IsEmpty() {
		return
	}
	text, err := sum.Daily(ctx, day, src)
	if err != nil {
		slog.Error("daily summary failed", "user_id", userID, "day", day.Format("2006-01-02"), "error", err)
		stats.failed.Add(1)
		return
	}
	if err := store.UpsertSummary(ctx, model.Summary{
		UserID: userID, Period: model.PeriodDaily, PeriodStart: day, Text: text, Model: client.Model(),
	}); err != nil {
		slog.Error("daily upsert failed", "user_id", userID, "day", day.Format("2006-01-02"), "error", err)
		stats.failed.Add(1)
		return
	}
	stats.daily.Add(1)
}

// writeWeekly backfills any missing dailies for the previous Monday-Sunday week
// and then summarizes the week from them.
func writeWeekly(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, userID string, loc *time.Location, now time.Time, stats *runStats) {
	weekStart := mondayOfPreviousWeek(now.In(loc))
	existing, err := store.Summaries(ctx, userID, model.PeriodDaily, weekStart, weekStart.AddDate(0, 0, 6))
	if err != nil {
		slog.Error("weekly lookup failed", "user_id", userID, "error", err)
		stats.failed.Add(1)
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
			text = backfillDaily(ctx, store, sum, client, userID, loc, day, stats)
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
		slog.Error("weekly summary failed", "user_id", userID, "week", weekStart.Format("2006-01-02"), "error", err)
		stats.failed.Add(1)
		return
	}
	if err := store.UpsertSummary(ctx, model.Summary{
		UserID: userID, Period: model.PeriodWeekly, PeriodStart: weekStart, Text: text, Model: client.Model(),
	}); err != nil {
		slog.Error("weekly upsert failed", "user_id", userID, "week", weekStart.Format("2006-01-02"), "error", err)
		stats.failed.Add(1)
		return
	}
	stats.weekly.Add(1)
}

// backfillDaily generates and stores a missing daily recap, returning "" when
// the day has no transcripts.
func backfillDaily(ctx context.Context, store *storage.PostgresStore, sum summarizer.Summarizer, client *llm.Client, userID string, loc *time.Location, day time.Time, stats *runStats) string {
	src, err := store.DaySources(ctx, userID, loc, day)
	if err != nil {
		slog.Error("backfill sources failed", "user_id", userID, "day", day.Format("2006-01-02"), "error", err)
		stats.failed.Add(1)
		return ""
	}
	if src.IsEmpty() {
		return ""
	}
	text, err := sum.Daily(ctx, day, src)
	if err != nil {
		slog.Error("backfill daily failed", "user_id", userID, "day", day.Format("2006-01-02"), "error", err)
		stats.failed.Add(1)
		return ""
	}
	if err := store.UpsertSummary(ctx, model.Summary{
		UserID: userID, Period: model.PeriodDaily, PeriodStart: day, Text: text, Model: client.Model(),
	}); err != nil {
		slog.Error("backfill upsert failed", "user_id", userID, "day", day.Format("2006-01-02"), "error", err)
		stats.failed.Add(1)
		return ""
	}
	stats.daily.Add(1)
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
		slog.Warn("invalid stored timezone; using default", "timezone", user.Timezone, "user_id", user.UserID)
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
