package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"checkpoint/ingestion/internal/repository"
)

// Request records a deletion tombstone for the user. Repeats are no-ops so
// a retried DELETE /v1/me stays idempotent.
func (p *Pool) Request(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO account_deletions (user_id) VALUES ($1)
			ON CONFLICT (user_id) DO NOTHING`, userID)
		return err
	})
}

// IsDeleting reports whether a tombstone exists for the user.
func (p *Pool) IsDeleting(ctx context.Context, userID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var deleting bool
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM account_deletions WHERE user_id = $1)`, userID).
			Scan(&deleting)
	})
	return deleting, err
}

// ClaimDeletions leases due tombstones to this worker.
func (p *Pool) ClaimDeletions(ctx context.Context, batch int, now time.Time) ([]repository.AccountDeletionJob, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := p.inner.Query(ctx, `
		WITH due AS (
			SELECT user_id FROM account_deletions
			WHERE status IN ('PENDING', 'PROCESSING') AND next_attempt_at <= $1
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE account_deletions d
		SET status = 'PROCESSING', attempt_count = d.attempt_count + 1, updated_at = $1
		FROM due
		WHERE d.user_id = due.user_id
		RETURNING d.user_id, d.attempt_count`, now, batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []repository.AccountDeletionJob
	for rows.Next() {
		var job repository.AccountDeletionJob
		if err := rows.Scan(&job.UserID, &job.Attempt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// CompleteDeletion marks a tombstone COMPLETE; the row is retained as
// security metadata so stale queued work can be rejected.
func (p *Pool) CompleteDeletion(ctx context.Context, userID string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := p.inner.Exec(ctx, `
		UPDATE account_deletions
		SET status = 'COMPLETE', completed_at = $2, updated_at = $2, last_error = NULL
		WHERE user_id = $1`, userID, now)
	return err
}

// RetryDeletion schedules another attempt after a failure.
func (p *Pool) RetryDeletion(ctx context.Context, userID string, next time.Time, errMsg string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := p.inner.Exec(ctx, `
		UPDATE account_deletions
		SET status = 'PENDING', next_attempt_at = $2, last_error = $3, updated_at = $4
		WHERE user_id = $1`, userID, next, errMsg, now)
	return err
}

// ListUploadObjects returns the stored objects owned by the user.
func (p *Pool) ListUploadObjects(ctx context.Context, userID string) ([]repository.UploadObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := p.inner.Query(ctx, `
		SELECT bucket, object_key FROM uploads WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var objects []repository.UploadObject
	for rows.Next() {
		var object repository.UploadObject
		if err := rows.Scan(&object.Bucket, &object.ObjectKey); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, rows.Err()
}

// PurgeIngestion removes the user's upload, outbox and idempotency rows.
func (p *Pool) PurgeIngestion(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	statements := []string{
		`DELETE FROM outbox_events WHERE aggregate_id IN (SELECT id FROM uploads WHERE user_id = $1)`,
		`DELETE FROM idempotency_keys WHERE user_id = $1`,
		`DELETE FROM user_notification_settings WHERE user_id = $1`,
		`DELETE FROM mcp_access_keys WHERE user_id = $1`,
		`DELETE FROM user_settings WHERE user_id = $1`,
		`DELETE FROM uploads WHERE user_id = $1`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// QuarantineDevice detaches the user's pendant and requires physical recovery.
func (p *Pool) QuarantineDevice(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := p.inner.Exec(ctx, `
		UPDATE devices SET state = 'reset_required', user_id = NULL, claimed_at = NULL, updated_at = NOW()
		WHERE user_id = $1`, userID)
	return err
}

// PurgeDownstream removes the user's transcripts, embeddings, extraction
// output (extraction_jobs + todos), summaries, MCP OAuth tokens and pending
// retries. The tables may live in a separate database in split deployments,
// so a missing table is treated as nothing to purge.
func (p *Pool) PurgeDownstream(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, table := range []string{"transcripts", "embeddings", "extraction_jobs", "todos", "reminders", "insights", "summaries", "notification_deliveries", "search_documents", "oauth_tokens", "oauth_authorization_codes", "retry_jobs"} {
		exists, err := tableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE user_id = $1`, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func tableExists(ctx context.Context, tx pgx.Tx, name string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&exists)
	return exists, err
}
