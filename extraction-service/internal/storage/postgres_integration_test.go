package storage

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"extraction-service/internal/model"
)

// Integration test against a real Postgres; skips without TEST_DATABASE_URL
// (see docs/DEVELOPER_SETUP.md). Expects the app schema to be migrated.
func newTestStore(t *testing.T) (*PostgresStore, *pgxpool.Pool) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	store, err := NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(store.Close)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store, pool
}

func seedTombstone(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM account_deletions WHERE user_id = $1`, userID); err != nil {
		t.Skipf("account_deletions schema mismatch: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO account_deletions (user_id) VALUES ($1::uuid)`, userID); err != nil {
		t.Skipf("account_deletions schema mismatch: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM account_deletions WHERE user_id = $1`, userID)
	})
}

func TestIsUserDeletingReportsTombstone(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "f2b5d0a4-0000-4000-8000-00000000e2e8"
	seedTombstone(t, pool, userID)

	deleting, err := store.IsUserDeleting(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleting {
		t.Fatal("expected tombstone to be reported")
	}
}

func TestIsUserDeletingFalseWithoutTombstone(t *testing.T) {
	store, _ := newTestStore(t)
	deleting, err := store.IsUserDeleting(context.Background(),
		"cf6f95e3-0000-4000-8000-00000000c6de")
	if err != nil {
		t.Fatal(err)
	}
	if deleting {
		t.Fatal("expected no tombstone")
	}
}

func TestCompleteBatchSkipsTombstonedUser(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "7c4a1f6e-0000-4000-8000-00000000ba7e"
	audioID := "7c4a1f6e-0000-4000-8000-00000000ba7f"
	seedTombstone(t, pool, userID)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM todos WHERE user_id = $1`, userID)
	})

	if err := store.CompleteBatch(ctx, []model.Result{{
		UserID: userID, AudioID: audioID, Todos: []string{"resurrected"},
	}}, "test-model"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM todos WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tombstoned user must not get todos, found %d", count)
	}
}

func TestReadyUsersExcludesTombstonedUsers(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "9d1fc2b2-0000-4000-8000-0000000de19d"
	seedTombstone(t, pool, userID)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM extraction_jobs WHERE user_id = $1`, userID)
	})
	if err := store.EnqueueJob(ctx, model.Job{
		UserID: userID, AudioID: userID, ExtractionType: model.TypeAll, Text: "test transcript",
	}); err != nil {
		t.Fatal(err)
	}

	users, err := store.ReadyUsers(ctx, model.TypeAll, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u == userID {
			t.Fatalf("tombstoned user %s must not be ready", userID)
		}
	}
}

// TestCompleteBatchPersistsInsights guards the insight writer against
// passing the model.Insight struct where pgx expects the text column.
func TestCompleteBatchPersistsInsights(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "3b8e1c2a-0000-4000-8000-0000000001a2"
	audioID := "3b8e1c2a-0000-4000-8000-0000000001a3"

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM insights WHERE user_id = $1`, userID)
	})

	if err := store.CompleteBatch(ctx, []model.Result{{
		UserID: userID, AudioID: audioID,
		Insights: []model.Insight{{Text: "the vendor discount was already in the quote"}},
	}}, "test-model"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM insights WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 insight row, got %d", count)
	}
}

func countUserRows(t *testing.T, pool *pgxpool.Pool, table, userID string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return count
}

// TestCompleteBatchMarksEmptyResultSkippedAndClears verifies the combined
// writer always replaces all three tables and records an empty run as
// 'skipped' rather than 'done'.
func TestCompleteBatchMarksEmptyResultSkippedAndClears(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "5e2b1a90-0000-4000-8000-00000000ab11"
	audioID := "5e2b1a90-0000-4000-8000-00000000ab12"
	tables := []string{"todos", "reminders", "insights"}

	t.Cleanup(func() {
		for _, table := range tables {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE user_id = $1`, userID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM extraction_jobs WHERE user_id = $1`, userID)
	})

	for _, table := range tables {
		if _, err := pool.Exec(ctx,
			`INSERT INTO `+table+` (user_id, audio_id, text, model) VALUES ($1, $2, 'stale', 'test')`,
			userID, audioID); err != nil {
			t.Skipf("%s schema mismatch: %v", table, err)
		}
	}

	if err := store.EnqueueJob(ctx, model.Job{
		UserID: userID, AudioID: audioID, ExtractionType: model.TypeAll, Text: "test transcript",
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimBatch(ctx, userID, model.TypeAll, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v jobs=%d", err, len(claimed))
	}

	if err := store.CompleteBatch(ctx, []model.Result{{
		JobID: claimed[0].ID, UserID: userID, AudioID: audioID,
	}}, "test-model"); err != nil {
		t.Fatal(err)
	}

	for _, table := range tables {
		if count := countUserRows(t, pool, table, userID); count != 0 {
			t.Fatalf("%s not cleared for empty result: %d rows", table, count)
		}
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM extraction_jobs WHERE id = $1`, claimed[0].ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "skipped" {
		t.Fatalf("status = %q, want %q", status, "skipped")
	}
}

func TestUserTimezoneReadsSettings(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "6a1c9f30-0000-4000-8000-00000000cd01"

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id = $1`, userID)
	})

	got, err := store.UserTimezone(ctx, userID)
	if err != nil {
		t.Fatalf("missing row: %v", err)
	}
	if got != "" {
		t.Fatalf("missing row must yield empty timezone, got %q", got)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, timezone) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET timezone = EXCLUDED.timezone`,
		userID, "Europe/Berlin"); err != nil {
		t.Skipf("user_settings schema mismatch: %v", err)
	}

	got, err = store.UserTimezone(ctx, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "Europe/Berlin" {
		t.Fatalf("timezone = %q, want Europe/Berlin", got)
	}
}
