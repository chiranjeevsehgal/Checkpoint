package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"retry-service/internal/model"
)

// PostgresStore owns the durable retry queue (retry_jobs). The consumer
// upserts one row per (source_topic, original_event_id); the dispatcher
// claims due rows FOR UPDATE SKIP LOCKED, so concurrent workers can never
// claim the same rows and a crashed worker's claims get reclaimed.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) Close() {
	p.pool.Close()
}

// tombstoneMissingWarned keeps the "gating disabled" warning to one line per
// process instead of one per check.
var tombstoneMissingWarned sync.Once

// IsUserDeleting reports whether a deletion tombstone exists for the user.
// A missing table means the worker runs against a separate database, so
// there is nothing to gate on.
func (p *PostgresStore) IsUserDeleting(ctx context.Context, userID string) (bool, error) {
	var deleting bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM account_deletions WHERE user_id = $1::uuid)`, userID).Scan(&deleting)
	if err != nil {
		if p.missingTombstoneTable(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking tombstone for %s: %w", userID, err)
	}
	return deleting, nil
}

// missingTombstoneTable reports whether the error is the account_deletions
// table not existing (a worker pointed at a DB without the ingestion schema).
func (p *PostgresStore) missingTombstoneTable(err error) bool {
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		return false
	}
	tombstoneMissingWarned.Do(func() {
		log.Printf("account_deletions missing; deletion-tombstone gating disabled")
	})
	return true
}

// RecordFailure folds one handoff into the retry queue and reports what
// happened to the job plus its current attempt count:
//
//	scheduled  first failure — row created, retry scheduled for now+delay
//	rearmed    a dispatched retry failed again and attempts remain
//	failed     a dispatched retry failed again and attempts are exhausted
//	duplicate  a handoff arrived while still pending/in flight — logged, schedule stands
//	terminal   a handoff arrived for an already-failed job — ignored
//
// The row is locked FOR UPDATE inside a transaction so two consumers
// processing a duplicate handoff (rebalance redelivery) cannot interleave
// their state transitions.
func (p *PostgresStore) RecordFailure(ctx context.Context, rec model.FailureRecord, maxAttempts int, delay time.Duration) (model.RecordOutcome, int, error) {
	entryJSON, err := json.Marshal(rec.Entry)
	if err != nil {
		return 0, 0, fmt.Errorf("marshalling attempt entry: %w", err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("beginning record-failure tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	var status string
	var attempts int
	err = tx.QueryRow(ctx, `
		SELECT id, status, attempts FROM retry_jobs
		WHERE source_topic = $1 AND original_event_id = $2
		FOR UPDATE`, rec.SourceTopic, rec.OriginalEventID).Scan(&id, &status, &attempts)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `
			INSERT INTO retry_jobs (
				original_event_id, source_service, source_topic, message_key, user_id,
				original_payload, next_attempt_at, last_stage, last_error_code, last_error, attempt_log
			) VALUES (
				$1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::uuid, $6,
				now() + make_interval(secs => $7), $8, $9, $10, jsonb_build_array($11::jsonb)
			)`,
			rec.OriginalEventID, rec.SourceService, rec.SourceTopic, rec.MessageKey, rec.UserID,
			rec.OriginalPayload, int(delay.Seconds()),
			rec.Entry.Stage, rec.Entry.ErrorCode, truncateErr(rec.Entry.ErrorMessage), entryJSON); err != nil {
			return 0, 0, fmt.Errorf("inserting retry job: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, 0, fmt.Errorf("committing record-failure tx: %w", err)
		}
		return model.OutcomeScheduled, 0, nil

	case err != nil:
		return 0, 0, fmt.Errorf("locking retry job: %w", err)
	}

	switch status {
	case "failed":
		// Terminal rows are immutable; a late duplicate handoff changes
		// nothing. The commit releases the row lock.
		if err := tx.Commit(ctx); err != nil {
			return 0, 0, fmt.Errorf("committing record-failure tx: %w", err)
		}
		return model.OutcomeTerminal, attempts, nil

	case "dispatched":
		if attempts >= maxAttempts {
			if _, err := tx.Exec(ctx, `
				UPDATE retry_jobs SET
					status = 'failed', failed_at = now(),
					last_stage = $1, last_error_code = $2, last_error = $3,
					attempt_log = retry_jobs.attempt_log || $4::jsonb,
					updated_at = now()
				WHERE id = $5`,
				rec.Entry.Stage, rec.Entry.ErrorCode, truncateErr(rec.Entry.ErrorMessage), entryJSON, id); err != nil {
				return 0, 0, fmt.Errorf("failing retry job: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return 0, 0, fmt.Errorf("committing record-failure tx: %w", err)
			}
			return model.OutcomeFailed, attempts, nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE retry_jobs SET
				status = 'pending',
				next_attempt_at = now() + make_interval(secs => $1),
				last_stage = $2, last_error_code = $3, last_error = $4,
				attempt_log = retry_jobs.attempt_log || $5::jsonb,
				updated_at = now()
			WHERE id = $6`,
			int(delay.Seconds()), rec.Entry.Stage, rec.Entry.ErrorCode, truncateErr(rec.Entry.ErrorMessage), entryJSON, id); err != nil {
			return 0, 0, fmt.Errorf("re-arming retry job: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, 0, fmt.Errorf("committing record-failure tx: %w", err)
		}
		return model.OutcomeRearmed, attempts, nil

	default: // pending or processing: duplicate handoff before a dispatch
		if _, err := tx.Exec(ctx, `
			UPDATE retry_jobs SET
				last_stage = $1, last_error_code = $2, last_error = $3,
				attempt_log = retry_jobs.attempt_log || $4::jsonb,
				updated_at = now()
			WHERE id = $5`,
			rec.Entry.Stage, rec.Entry.ErrorCode, truncateErr(rec.Entry.ErrorMessage), entryJSON, id); err != nil {
			return 0, 0, fmt.Errorf("updating retry job: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, 0, fmt.Errorf("committing record-failure tx: %w", err)
		}
		return model.OutcomeDuplicate, attempts, nil
	}
}

// ClaimDue atomically moves up to n due pending rows into 'processing' and
// returns them, oldest next_attempt_at first. SKIP LOCKED makes concurrent
// dispatcher workers safe. Attempts is deliberately NOT incremented here —
// unlike extraction's batch claim, a claim that dies in a broker outage
// must not consume a retry attempt, so the count advances only in
// MarkDispatched once the payload has actually been re-published.
func (p *PostgresStore) ClaimDue(ctx context.Context, n int) ([]model.RetryJob, error) {
	rows, err := p.pool.Query(ctx, `
		UPDATE retry_jobs SET status = 'processing', updated_at = now()
		WHERE id IN (
			SELECT id FROM retry_jobs
			WHERE status = 'pending' AND next_attempt_at <= now()
			ORDER BY next_attempt_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, original_event_id, source_service, source_topic, message_key, user_id, original_payload, attempts`,
		n)
	if err != nil {
		return nil, fmt.Errorf("claiming due retry jobs: %w", err)
	}
	defer rows.Close()

	var jobs []model.RetryJob
	for rows.Next() {
		var j model.RetryJob
		var messageKey, userID *string
		if err := rows.Scan(&j.ID, &j.OriginalEventID, &j.SourceService, &j.SourceTopic, &messageKey, &userID, &j.OriginalPayload, &j.Attempts); err != nil {
			return nil, fmt.Errorf("scanning claimed retry job: %w", err)
		}
		if messageKey != nil {
			j.MessageKey = *messageKey
		}
		if userID != nil {
			j.UserID = *userID
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// MarkDispatched records a completed re-delivery: the payload is back on the
// source topic and the attempt count advances. The status guard makes a
// stale claim (reclaimed after a crash) a no-op; the row will simply be
// re-dispatched, which is at-least-once like everything else on the bus.
func (p *PostgresStore) MarkDispatched(ctx context.Context, id string) error {
	if _, err := p.pool.Exec(ctx, `
		UPDATE retry_jobs
		SET status = 'dispatched', attempts = attempts + 1, dispatched_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'processing'`, id); err != nil {
		return fmt.Errorf("marking retry job %s dispatched: %w", id, err)
	}
	return nil
}

// ReleaseClaim returns a claimed row to pending after a failed publish
// (broker down). The schedule is left untouched, so the next poll retries
// immediately; broker errors are retried indefinitely by design and never
// consume a retry attempt.
func (p *PostgresStore) ReleaseClaim(ctx context.Context, id string) error {
	if _, err := p.pool.Exec(ctx, `
		UPDATE retry_jobs SET status = 'pending', updated_at = now()
		WHERE id = $1 AND status = 'processing'`, id); err != nil {
		return fmt.Errorf("releasing retry job %s: %w", id, err)
	}
	return nil
}

// MarkSkipped abandons a claimed row whose account was deleted: a tombstoned
// user's events must never be re-delivered to the workers.
func (p *PostgresStore) MarkSkipped(ctx context.Context, id string) error {
	if _, err := p.pool.Exec(ctx, `
		UPDATE retry_jobs SET status = 'skipped', updated_at = now()
		WHERE id = $1 AND status = 'processing'`, id); err != nil {
		return fmt.Errorf("skipping retry job %s: %w", id, err)
	}
	return nil
}

// ReclaimStale resets rows stuck in 'processing' (crashed workers, lost
// pods) back to pending. Safe to run against live workers as long as
// reclaimAfter exceeds the longest publish.
func (p *PostgresStore) ReclaimStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE retry_jobs SET status = 'pending', updated_at = now()
		WHERE status = 'processing' AND updated_at < now() - make_interval(secs => $1)`,
		int(olderThan.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("reclaiming stale retry jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PruneTerminal deletes rows whose story is over — dispatched (no further
// handoff arrived), failed or skipped — once they are older than the
// retention window, so the table stays inspectable without growing forever.
func (p *PostgresStore) PruneTerminal(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM retry_jobs
		WHERE status IN ('dispatched','failed','skipped')
		  AND updated_at < now() - make_interval(secs => $1)`,
		int(olderThan.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("pruning terminal retry jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func truncateErr(msg string) string {
	const max = 500
	if len(msg) > max {
		return msg[:max]
	}
	return msg
}
