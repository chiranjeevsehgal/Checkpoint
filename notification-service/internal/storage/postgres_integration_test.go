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

func advanceHasUser(candidates []model.Candidate, userID string) bool {
	for _, c := range candidates {
		if c.UserID == userID {
			return true
		}
	}
	return false
}

func advanceLead(candidates []model.Candidate, userID string) int {
	for _, c := range candidates {
		if c.UserID == userID {
			return c.AdvanceSeconds
		}
	}
	return -1
}

func TestDueCandidatesAndClaim(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "f1e2d3c4-0000-4000-8000-00000000c0a1"
	audioID := "f1e2d3c4-0000-4000-8000-00000000c0a2"
	shortID := "f1e2d3c4-0000-4000-8000-00000000c0a3"

	for _, table := range []string{"reminders", "user_notification_settings", "notification_deliveries"} {
		if !hasTable(t, pool, table) {
			t.Skip("app schema not migrated")
		}
	}
	t.Cleanup(func() {
		for _, user := range []string{userID, shortID} {
			_, _ = pool.Exec(ctx, `DELETE FROM notification_deliveries WHERE user_id = $1`, user)
			_, _ = pool.Exec(ctx, `DELETE FROM reminders WHERE user_id = $1`, user)
			_, _ = pool.Exec(ctx, `DELETE FROM user_notification_settings WHERE user_id = $1`, user)
		}
	})

	// Advance candidate: due in 14 minutes, default 15-minute lead (fire 1 min ago).
	if _, err := pool.Exec(ctx, `
		INSERT INTO reminders (user_id, audio_id, text, remind_at, model, important)
		VALUES ($1, $2, 'advance me', now() + interval '14 minutes', 'test', true)`, userID, audioID); err != nil {
		t.Fatalf("seed important reminder: %v", err)
	}
	// Suppressed: same timing but not important, so no advance.
	if _, err := pool.Exec(ctx, `
		INSERT INTO reminders (user_id, audio_id, text, remind_at, model, important)
		VALUES ($1, $2, 'quiet', now() + interval '14 minutes', 'test', false)`, userID, audioID+"b"); err != nil {
		t.Fatalf("seed unimportant reminder: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_notification_settings (user_id, ntfy_topic, enabled)
		VALUES ($1, 'cp-test', true)`, userID); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	// Per-user 5-minute lead: due in 4 minutes (fire 1 min ago).
	if _, err := pool.Exec(ctx, `
		INSERT INTO reminders (user_id, audio_id, text, remind_at, model, important)
		VALUES ($1, $2, 'short lead', now() + interval '4 minutes', 'test', true)`, shortID, audioID+"c"); err != nil {
		t.Fatalf("seed short-lead reminder: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_notification_settings (user_id, ntfy_topic, enabled, advance_seconds)
		VALUES ($1, 'cp-test', true, 300)`, shortID); err != nil {
		t.Fatalf("seed short-lead settings: %v", err)
	}

	advance, err := store.DueCandidates(ctx, model.KindAdvance, 15*time.Minute, 2*time.Minute, time.Hour, 30*time.Minute, 20)
	if err != nil {
		t.Fatalf("advance candidates: %v", err)
	}
	if !advanceHasUser(advance, userID) {
		t.Fatal("important reminder should be an advance candidate")
	}
	if !advanceHasUser(advance, shortID) {
		t.Fatal("short-lead reminder should be an advance candidate")
	}
	for _, c := range advance {
		if c.UserID == userID && c.ReminderText == "quiet" {
			t.Fatal("unimportant reminder must not produce an advance candidate")
		}
	}
	if got := advanceLead(advance, userID); got != 900 {
		t.Fatalf("default lead = %d seconds, want 900", got)
	}
	if got := advanceLead(advance, shortID); got != 300 {
		t.Fatalf("short lead = %d seconds, want 300", got)
	}

	due, err := store.DueCandidates(ctx, model.KindDue, 15*time.Minute, 2*time.Minute, time.Hour, 30*time.Minute, 20)
	if err != nil {
		t.Fatalf("due candidates: %v", err)
	}
	if advanceHasUser(due, userID) {
		t.Fatal("future reminder must not be a due candidate")
	}

	candidate := model.Candidate{
		UserID: userID, AudioID: audioID, ReminderText: "advance me", Kind: model.KindDue,
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

func TestReclaimAfterReleasesStaleProcessing(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "f1e2d3c4-0000-4000-8000-00000000c0b1"
	audioID := "f1e2d3c4-0000-4000-8000-00000000c0b2"

	for _, table := range []string{"notification_deliveries"} {
		if !hasTable(t, pool, table) {
			t.Skip("app schema not migrated")
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM notification_deliveries WHERE user_id = $1`, userID)
	})

	candidate := model.Candidate{
		UserID: userID, AudioID: audioID, ReminderText: "reclaim me", Kind: model.KindDue,
		FireAt: time.Now().Add(-time.Minute), Topic: "cp-test",
	}
	id, _, claimed, err := store.ClaimDelivery(ctx, candidate, 5, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("first claim: id=%d claimed=%v err=%v", id, claimed, err)
	}
	// A live processing row cannot be claimed twice.
	if _, _, claimed, err = store.ClaimDelivery(ctx, candidate, 5, time.Minute); err != nil || claimed {
		t.Fatalf("fresh processing row must not be reclaimed: claimed=%v err=%v", claimed, err)
	}
	// Age the row past the reclaim window: it becomes claimable again.
	if _, err := pool.Exec(ctx, `UPDATE notification_deliveries SET updated_at = now() - interval '2 minutes' WHERE id = $1`, id); err != nil {
		t.Fatalf("age row: %v", err)
	}
	id2, attempts, claimed, err := store.ClaimDelivery(ctx, candidate, 5, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("stale row must be reclaimed: claimed=%v err=%v", claimed, err)
	}
	if id2 != id {
		t.Fatalf("reclaimed id = %d, want %d", id2, id)
	}
	if attempts != 1 {
		t.Fatalf("reclaimed attempts = %d, want 1", attempts)
	}
}
