package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rollup-service/internal/model"
)

// Integration test against a real Postgres; skips without TEST_DATABASE_URL
// (see docs/DEVELOPER_SETUP.md). Expects the app schema to be migrated.
func newTestStore(t *testing.T) (*PostgresStore, *pgxpool.Pool) {
	t.Helper()
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

func hasTable(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestActiveUsersReturnsTimezone(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "d4e5f6a7-0000-4000-8000-00000000b1c1"
	audioID := "d4e5f6a7-0000-4000-8000-00000000b1c2"

	if !hasTable(t, pool, "transcripts") || !hasTable(t, pool, "user_settings") {
		t.Skip("app schema not migrated")
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM transcripts WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id = $1`, userID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, timezone) VALUES ($1, 'Europe/Berlin')
		ON CONFLICT (user_id) DO UPDATE SET timezone = EXCLUDED.timezone`, userID); err != nil {
		t.Fatalf("seeding user_settings: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO transcripts (audio_id, user_id, text, recorded_at)
		VALUES ($1, $2, 't', $3)`, audioID, userID, time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seeding transcripts: %v", err)
	}

	users, err := store.ActiveUsers(ctx, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range users {
		if u.UserID == userID {
			found = true
			if u.Timezone != "Europe/Berlin" {
				t.Fatalf("timezone = %q, want Europe/Berlin", u.Timezone)
			}
		}
	}
	if !found {
		t.Fatalf("active user %s not returned", userID)
	}
}

func TestDaySourcesBucketsByLocalDay(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "e5f6a7b8-0000-4000-8000-00000000c1d1"
	audioID := "e5f6a7b8-0000-4000-8000-00000000c1d2"

	if !hasTable(t, pool, "transcripts") || !hasTable(t, pool, "todos") {
		t.Skip("app schema not migrated")
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM transcripts WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM todos WHERE user_id = $1`, userID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO transcripts (audio_id, user_id, text, recorded_at)
		VALUES ($1, $2, 'launch discussion', $3)`,
		audioID, userID, time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seeding transcripts: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO todos (user_id, audio_id, text, model) VALUES ($1, $2, 'send report', 'test')`,
		userID, audioID); err != nil {
		t.Fatalf("seeding todos: %v", err)
	}

	day := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	src, err := store.DaySources(ctx, userID, time.UTC, day)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Transcripts) != 1 || src.Transcripts[0] != "launch discussion" {
		t.Fatalf("transcripts wrong: %+v", src.Transcripts)
	}
	if len(src.Todos) != 1 || src.Todos[0] != "send report" {
		t.Fatalf("todos wrong: %+v", src.Todos)
	}

	other, err := store.DaySources(ctx, userID, time.UTC, day.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if !other.IsEmpty() {
		t.Fatalf("adjacent day should be empty: %+v", other)
	}
}

func TestUpsertSummaryReplacesExisting(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "f6a7b8c9-0000-4000-8000-00000000d1e1"
	day := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)

	if !hasTable(t, pool, "summaries") {
		t.Skip("run rollup migrations to create summaries")
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM summaries WHERE user_id = $1`, userID)
	})

	if err := store.UpsertSummary(ctx, model.Summary{
		UserID: userID, Period: model.PeriodDaily, PeriodStart: day, Text: "first", Model: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSummary(ctx, model.Summary{
		UserID: userID, Period: model.PeriodDaily, PeriodStart: day, Text: "second", Model: "test",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := store.Summaries(ctx, userID, model.PeriodDaily, day, day)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "second" {
		t.Fatalf("expected one replaced row, got %+v", got)
	}
}

func TestIsUserDeletingReportsTombstone(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "a7b8c9d0-0000-4000-8000-00000000e1f1"

	if !hasTable(t, pool, "account_deletions") {
		t.Skip("account_deletions not migrated")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM account_deletions WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("clearing tombstone: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO account_deletions (user_id) VALUES ($1::uuid)`, userID); err != nil {
		t.Fatalf("seeding tombstone: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM account_deletions WHERE user_id = $1`, userID)
	})

	deleting, err := store.IsUserDeleting(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleting {
		t.Fatal("expected tombstone to be reported")
	}
}
