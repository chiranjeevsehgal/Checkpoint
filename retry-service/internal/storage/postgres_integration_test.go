package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"retry-service/internal/model"
)

// Integration test against a real Postgres; skips without TEST_DATABASE_URL.
// Expects the retry schema to be migrated (go run ./cmd/migrate up).
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

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.retry_jobs') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Skip("retry schema not migrated")
	}
	return store, pool
}

// newHandoff builds a failure record with a unique original event id and
// registers its cleanup.
func newHandoff(t *testing.T, pool *pgxpool.Pool, stage string) model.FailureRecord {
	t.Helper()
	eventID := uuid.NewString()
	userID := uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM retry_jobs WHERE original_event_id = $1`, eventID)
	})
	return model.FailureRecord{
		OriginalEventID: eventID,
		SourceService:   "transcription-service",
		SourceTopic:     "transcription.jobs.v1",
		MessageKey:      userID,
		UserID:          userID,
		OriginalPayload: []byte(`{"schema_version":2,"event_id":"` + eventID + `","event_type":"TRANSCRIPTION_REQUESTED","data":{"user_id":"` + userID + `"}}`),
		Entry: model.AttemptEntry{
			AttemptedAt:  time.Now().UTC().Format(time.RFC3339),
			Stage:        stage,
			ErrorCode:    "PROVIDER_5XX",
			ErrorMessage: "provider returned 502",
		},
	}
}

func jobStatus(t *testing.T, pool *pgxpool.Pool, originalEventID string) (string, int) {
	t.Helper()
	var status string
	var attempts int
	if err := pool.QueryRow(context.Background(),
		`SELECT status, attempts FROM retry_jobs WHERE original_event_id = $1`, originalEventID).Scan(&status, &attempts); err != nil {
		t.Fatalf("reading retry job: %v", err)
	}
	return status, attempts
}

// TestRecordFailureLifecycle walks the full happy-then-terminal path:
// schedule → dispatch → rearm → dispatch → fail → terminal, asserting the
// attempt accounting and that a terminal row is never dispatched again.
func TestRecordFailureLifecycle(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()
	const maxAttempts = 2
	// Zero delay makes every schedule immediately due.
	const delay = 0 * time.Second

	rec := newHandoff(t, pool, "provider_call")

	outcome, attempts, err := store.RecordFailure(ctx, rec, maxAttempts, delay)
	if err != nil || outcome != model.OutcomeScheduled || attempts != 0 {
		t.Fatalf("first record: outcome=%v attempts=%d err=%v", outcome, attempts, err)
	}
	if status, n := jobStatus(t, pool, rec.OriginalEventID); status != "pending" || n != 0 {
		t.Fatalf("after first record: status=%s attempts=%d", status, n)
	}

	// Dispatch 1: claim → publish → mark.
	jobs, err := store.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("claim 1: %v", err)
	}
	if len(jobs) != 1 || jobs[0].OriginalEventID != rec.OriginalEventID {
		t.Fatalf("claim 1 should return the scheduled job, got %d jobs", len(jobs))
	}
	job := jobs[0]
	if job.Attempts != 0 || job.SourceService != rec.SourceService || job.UserID != rec.UserID || job.MessageKey != rec.MessageKey {
		t.Fatalf("claimed job fields wrong: %+v", job)
	}
	if string(job.OriginalPayload) != string(rec.OriginalPayload) {
		t.Fatalf("original payload not byte-identical: %s", job.OriginalPayload)
	}
	if err := store.MarkDispatched(ctx, job.ID); err != nil {
		t.Fatalf("mark dispatched 1: %v", err)
	}
	if status, n := jobStatus(t, pool, rec.OriginalEventID); status != "dispatched" || n != 1 {
		t.Fatalf("after dispatch 1: status=%s attempts=%d", status, n)
	}

	// The retried message failed again: rearm with attempts left.
	outcome, attempts, err = store.RecordFailure(ctx, rec, maxAttempts, delay)
	if err != nil || outcome != model.OutcomeRearmed || attempts != 1 {
		t.Fatalf("second record: outcome=%v attempts=%d err=%v", outcome, attempts, err)
	}

	// Dispatch 2.
	jobs, err = store.ClaimDue(ctx, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim 2: jobs=%d err=%v", len(jobs), err)
	}
	if err := store.MarkDispatched(ctx, jobs[0].ID); err != nil {
		t.Fatalf("mark dispatched 2: %v", err)
	}
	if status, n := jobStatus(t, pool, rec.OriginalEventID); status != "dispatched" || n != 2 {
		t.Fatalf("after dispatch 2: status=%s attempts=%d", status, n)
	}

	// Third failure: attempts exhausted → terminal failed.
	outcome, attempts, err = store.RecordFailure(ctx, rec, maxAttempts, delay)
	if err != nil || outcome != model.OutcomeFailed || attempts != 2 {
		t.Fatalf("third record: outcome=%v attempts=%d err=%v", outcome, attempts, err)
	}
	if status, n := jobStatus(t, pool, rec.OriginalEventID); status != "failed" || n != 2 {
		t.Fatalf("after terminal: status=%s attempts=%d", status, n)
	}

	// A late duplicate handoff for a terminal row changes nothing.
	outcome, _, err = store.RecordFailure(ctx, rec, maxAttempts, delay)
	if err != nil || outcome != model.OutcomeTerminal {
		t.Fatalf("late handoff: outcome=%v err=%v", outcome, err)
	}
	if status, n := jobStatus(t, pool, rec.OriginalEventID); status != "failed" || n != 2 {
		t.Fatalf("terminal row mutated: status=%s attempts=%d", status, n)
	}

	// A terminal row is never claimed again.
	jobs, err = store.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("final claim: %v", err)
	}
	for _, j := range jobs {
		if j.OriginalEventID == rec.OriginalEventID {
			t.Fatal("terminal row must not be claimed")
		}
	}
}

// TestRecordFailureDuplicateBeforeDispatch covers a handoff arriving while a
// retry is still pending (duplicate publish): the failure is logged but the
// schedule stands and no attempt is consumed.
func TestRecordFailureDuplicateBeforeDispatch(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()

	rec := newHandoff(t, pool, "download_audio")
	rec.Entry.Stage = "download_audio"

	if _, _, err := store.RecordFailure(ctx, rec, 2, time.Hour); err != nil {
		t.Fatalf("first record: %v", err)
	}
	outcome, attempts, err := store.RecordFailure(ctx, rec, 2, time.Hour)
	if err != nil || outcome != model.OutcomeDuplicate || attempts != 0 {
		t.Fatalf("duplicate record: outcome=%v attempts=%d err=%v", outcome, attempts, err)
	}
	if status, n := jobStatus(t, pool, rec.OriginalEventID); status != "pending" || n != 0 {
		t.Fatalf("duplicate mutated schedule: status=%s attempts=%d", status, n)
	}

	// Not due for an hour, so nothing is claimable.
	if jobs, err := store.ClaimDue(ctx, 10); err != nil || len(jobs) != 0 {
		t.Fatalf("future row claimed: jobs=%d err=%v", len(jobs), err)
	}
}

// TestReleaseClaimReturnsRowForImmediateRetry covers the broker-outage
// path: a claimed row whose publish failed goes back to pending with its
// schedule untouched and its attempt count unchanged.
func TestReleaseClaimReturnsRowForImmediateRetry(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()

	rec := newHandoff(t, pool, "persist")
	if _, _, err := store.RecordFailure(ctx, rec, 2, 0); err != nil {
		t.Fatalf("record: %v", err)
	}

	jobs, err := store.ClaimDue(ctx, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: jobs=%d err=%v", len(jobs), err)
	}
	if err := store.ReleaseClaim(ctx, jobs[0].ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	if status, n := jobStatus(t, pool, rec.OriginalEventID); status != "pending" || n != 0 {
		t.Fatalf("after release: status=%s attempts=%d", status, n)
	}

	// Due again on the next poll, without having consumed an attempt.
	jobs, err = store.ClaimDue(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 0 {
		t.Fatalf("re-claim after release: jobs=%d err=%v", len(jobs), err)
	}
}

// TestReclaimStaleAndPruneTerminal covers crash recovery and retention.
func TestReclaimStaleAndPruneTerminal(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()

	stuck := newHandoff(t, pool, "provider_call")
	if _, _, err := store.RecordFailure(ctx, stuck, 2, 0); err != nil {
		t.Fatalf("record: %v", err)
	}
	if jobs, err := store.ClaimDue(ctx, 10); err != nil || len(jobs) != 1 {
		t.Fatalf("claim: jobs=%d err=%v", len(jobs), err)
	}
	// Simulate a crashed worker: the row sits in processing, backdated.
	if _, err := pool.Exec(ctx, `
		UPDATE retry_jobs SET updated_at = now() - interval '10 minutes'
		WHERE original_event_id = $1`, stuck.OriginalEventID); err != nil {
		t.Fatal(err)
	}
	if n, err := store.ReclaimStale(ctx, 5*time.Minute); err != nil || n != 1 {
		t.Fatalf("reclaim: n=%d err=%v", n, err)
	}
	if status, _ := jobStatus(t, pool, stuck.OriginalEventID); status != "pending" {
		t.Fatalf("stuck row not reclaimed: %s", status)
	}

	done := newHandoff(t, pool, "persist")
	if _, _, err := store.RecordFailure(ctx, done, 2, 0); err != nil {
		t.Fatalf("record: %v", err)
	}
	// The reclaimed row is also due again; claim everything and dispatch
	// only the row under test.
	jobs, err := store.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	var doneJob *model.RetryJob
	for i := range jobs {
		if jobs[i].OriginalEventID == done.OriginalEventID {
			doneJob = &jobs[i]
		}
	}
	if doneJob == nil {
		t.Fatalf("claim did not return the row under test (%d jobs)", len(jobs))
	}
	if err := store.MarkDispatched(ctx, doneJob.ID); err != nil {
		t.Fatalf("mark dispatched: %v", err)
	}
	// Backdate the dispatched row past the retention window.
	if _, err := pool.Exec(ctx, `
		UPDATE retry_jobs SET updated_at = now() - interval '40 days'
		WHERE original_event_id = $1`, done.OriginalEventID); err != nil {
		t.Fatal(err)
	}
	if n, err := store.PruneTerminal(ctx, 30*24*time.Hour); err != nil || n != 1 {
		t.Fatalf("prune: n=%d err=%v", n, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM retry_jobs WHERE original_event_id = $1`, done.OriginalEventID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("dispatched row past retention should be pruned")
	}
}

// TestClaimDueRespectsBatchAndOrder seeds two due rows and asserts only the
// oldest one is returned when the batch size is 1.
func TestClaimDueRespectsBatchAndOrder(t *testing.T) {
	store, pool := newTestStore(t)
	ctx := context.Background()

	older := newHandoff(t, pool, "a")
	if _, _, err := store.RecordFailure(ctx, older, 2, 0); err != nil {
		t.Fatalf("record older: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	newer := newHandoff(t, pool, "b")
	if _, _, err := store.RecordFailure(ctx, newer, 2, 0); err != nil {
		t.Fatalf("record newer: %v", err)
	}

	jobs, err := store.ClaimDue(ctx, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: jobs=%d err=%v", len(jobs), err)
	}
	if jobs[0].OriginalEventID != older.OriginalEventID {
		t.Fatal("batch of 1 should return the oldest due row first")
	}
}
