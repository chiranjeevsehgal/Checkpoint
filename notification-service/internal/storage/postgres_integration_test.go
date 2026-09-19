package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"notification-service/internal/model"
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

func TestDueCandidatesAndClaim(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "f1e2d3c4-0000-4000-8000-00000000c0a1"
	audioID := "f1e2d3c4-0000-4000-8000-00000000c0a2"
	text := "test reminder"

	for _, table := range []string{"reminders", "user_notification_settings", "notification_deliveries"} {
		if !hasTable(t, pool, table) {
			t.Skip("app schema not migrated")
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM notification_deliveries WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM reminders WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM user_notification_settings WHERE user_id = $1`, userID)
	})

	// A reminder due in 5 minutes is inside the advance window (not yet due).
	if _, err := pool.Exec(ctx, `
		INSERT INTO reminders (user_id, audio_id, text, remind_at, model)
		VALUES ($1, $2, $3, now() + interval '5 minutes', 'test')`, userID, audioID, text); err != nil {
		t.Fatalf("seed reminder: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_notification_settings (user_id, ntfy_topic, enabled)
		VALUES ($1, 'cp-test', true)`, userID); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	advance, err := store.DueCandidates(ctx, model.KindAdvance, 15*time.Minute, 2*time.Minute, time.Hour, 10)
	if err != nil {
		t.Fatalf("advance candidates: %v", err)
	}
	found := false
	for _, c := range advance {
		if c.UserID == userID && c.ReminderText == text {
			found = true
		}
	}
	if !found {
		t.Fatal("advance candidate not found")
	}

	// The due window has not opened yet.
	due, err := store.DueCandidates(ctx, model.KindDue, 15*time.Minute, 2*time.Minute, time.Hour, 10)
	if err != nil {
		t.Fatalf("due candidates: %v", err)
	}
	for _, c := range due {
		if c.UserID == userID {
			t.Fatal("reminder due in the future must not be a due candidate")
		}
	}

	candidate := model.Candidate{
		UserID: userID, AudioID: audioID, ReminderText: text, Kind: model.KindDue,
		FireAt: time.Now().Add(-time.Minute), Topic: "cp-test",
	}
	id, attempts, claimed, err := store.ClaimDelivery(ctx, candidate, 3, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim: id=%d attempts=%d claimed=%v err=%v", id, attempts, claimed, err)
	}
	if _, _, claimed, err = store.ClaimDelivery(ctx, candidate, 3, time.Minute); err != nil || claimed {
		t.Fatalf("second claim must lose: claimed=%v err=%v", claimed, err)
	}
	if err := store.MarkSent(ctx, id); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM notification_deliveries WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "sent" {
		t.Fatalf("status = %q, want sent", status)
	}
}
