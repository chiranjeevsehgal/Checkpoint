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

func TestReadyUsersExcludesTombstonedUsers(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	userID := "9d1fc2b2-0000-4000-8000-0000000de19d"
	seedTombstone(t, pool, userID)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM extraction_jobs WHERE user_id = $1`, userID)
	})
	if err := store.EnqueueJob(ctx, model.Job{
		UserID: userID, AudioID: userID, ExtractionType: "todo", Text: "test transcript",
	}); err != nil {
		t.Fatal(err)
	}

	users, err := store.ReadyUsers(ctx, "todo", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u == userID {
			t.Fatalf("tombstoned user %s must not be ready", userID)
		}
	}
}
