package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"extraction-service/internal/model"
)

// PostgresStore is the durable batch queue (extraction_jobs) plus the todo
// writer. The consumer goroutine only inserts pending rows; the batcher
// claims them FOR UPDATE SKIP LOCKED, so concurrent workers can never claim
// the same rows and a crashed worker's claims get reclaimed automatically.
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

// IsUserDeleting reports whether a deletion tombstone exists for the user.
// A missing table means downstream runs against a separate database, so
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

// EnqueueJob inserts one pending row per (user, audio, type). Idempotent
// under Kafka redelivery: a duplicate event is a no-op.
func (p *PostgresStore) EnqueueJob(ctx context.Context, j model.Job) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO extraction_jobs (user_id, audio_id, extraction_type, text, language, recorded_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::timestamptz)
		ON CONFLICT (user_id, audio_id, extraction_type) DO UPDATE SET
			recorded_at = COALESCE(extraction_jobs.recorded_at, EXCLUDED.recorded_at)`,
		j.UserID, j.AudioID, j.ExtractionType, j.Text, j.Language, j.RecordedAt)
	if err != nil {
		return fmt.Errorf("enqueueing extraction job: %w", err)
	}
	return nil
}

// ReadyUsers returns user_ids whose pending rows form a full batch, or —
// when maxWait > 0 — a partial batch whose oldest row has waited that long.
// One GROUP BY covers every user; nothing is tracked in memory. Deleted
// accounts are excluded so tombstoned transcripts never reach the LLM.
func (p *PostgresStore) ReadyUsers(ctx context.Context, extractionType string, batchSize int, maxWait time.Duration) ([]string, error) {
	users, err := p.readyUsers(ctx, true, extractionType, batchSize, maxWait)
	if err != nil && p.missingTombstoneTable(err) {
		users, err = p.readyUsers(ctx, false, extractionType, batchSize, maxWait)
	}
	if err != nil {
		return nil, fmt.Errorf("querying ready users: %w", err)
	}
	return users, nil
}

// readyUsers scans ReadyUsers' queue query; withTombstoneGate joins the
// account_deletions table to skip deleted accounts.
func (p *PostgresStore) readyUsers(ctx context.Context, withTombstoneGate bool, extractionType string, batchSize int, maxWait time.Duration) ([]string, error) {
	query := `
		SELECT user_id
		FROM extraction_jobs
		WHERE status = 'pending' AND extraction_type = $1`
	args := []interface{}{extractionType}
	if withTombstoneGate {
		query += `
		  AND NOT EXISTS (
			SELECT 1 FROM account_deletions tt
			WHERE tt.user_id = extraction_jobs.user_id::uuid
		  )`
	}
	query += `
		GROUP BY user_id
		HAVING count(*) >= $2`
	args = append(args, batchSize)
	if maxWait > 0 {
		query += ` OR min(created_at) <= now() - make_interval(secs => $3)`
		args = append(args, int(maxWait.Seconds()))
	}
	query += `
		ORDER BY min(created_at)`

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// missingTombstoneTable reports whether the error is the account_deletions
// table not existing (a worker pointed at a DB without the ingestion schema).
func (p *PostgresStore) missingTombstoneTable(err error) bool {
	var pgErr *pgconn.PgError
	return err != nil && errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

// ClaimBatch atomically moves up to n pending rows for one user+type into
// 'processing' and returns them, oldest first. SKIP LOCKED makes concurrent
// batcher workers safe; attempts is incremented here, before any work runs.
func (p *PostgresStore) ClaimBatch(ctx context.Context, userID, extractionType string, n int) ([]model.Job, error) {
	rows, err := p.pool.Query(ctx, `
		UPDATE extraction_jobs SET status = 'processing', attempts = attempts + 1, updated_at = now()
		WHERE id IN (
			SELECT id FROM extraction_jobs
			WHERE user_id = $1 AND extraction_type = $2 AND status = 'pending'
			ORDER BY created_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, user_id, audio_id, extraction_type, text, language, COALESCE(recorded_at::text, ''), attempts`,
		userID, extractionType, n)
	if err != nil {
		return nil, fmt.Errorf("claiming batch for user %s: %w", userID, err)
	}
	defer rows.Close()

	var jobs []model.Job
	for rows.Next() {
		var j model.Job
		if err := rows.Scan(&j.ID, &j.UserID, &j.AudioID, &j.ExtractionType, &j.Text, &j.Language, &j.Attempts); err != nil {
			return nil, fmt.Errorf("scanning claimed job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// CompleteBatch persists extraction results and marks their jobs done in
// one transaction. Todos are replaced per audio (DELETE then INSERT) so a
// redelivery or re-run can neither duplicate nor leave stale rows.
func (p *PostgresStore) CompleteBatch(ctx context.Context, results []model.Result, llmModel string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning complete-batch tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, r := range results {
		if _, err := tx.Exec(ctx, `DELETE FROM todos WHERE user_id = $1 AND audio_id = $2`, r.UserID, r.AudioID); err != nil {
			return fmt.Errorf("replacing todos for %s: %w", r.AudioID, err)
		}
		for _, text := range r.Todos {
			if _, err := tx.Exec(ctx, `
				INSERT INTO todos (user_id, audio_id, text, model, recorded_at)
				VALUES ($1, $2, $3, $4, NULLIF($5, '')::timestamptz)
				ON CONFLICT (user_id, audio_id, text) DO NOTHING`,
				r.UserID, r.AudioID, text, llmModel, r.RecordedAt); err != nil {
				return fmt.Errorf("inserting todo for %s: %w", r.AudioID, err)
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE extraction_jobs
			SET status = 'done', processed_at = now(), updated_at = now(), last_error = NULL
			WHERE id = $1`, r.JobID); err != nil {
			return fmt.Errorf("marking job %d done: %w", r.JobID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing complete-batch tx: %w", err)
	}
	return nil
}

// ReleaseJobs returns claimed rows to the pending pool after a retryable
// failure. The next poll re-claims them; attempts keeps counting up.
func (p *PostgresStore) ReleaseJobs(ctx context.Context, jobIDs []int64, errMsg string) error {
	if _, err := p.pool.Exec(ctx, `
		UPDATE extraction_jobs SET status = 'pending', last_error = $2, updated_at = now()
		WHERE id = ANY($1)`, jobIDs, truncateErr(errMsg)); err != nil {
		return fmt.Errorf("releasing jobs: %w", err)
	}
	return nil
}

// FailJobs gives up on rows that exhausted max_attempts. They stay visible
// with their last_error; requeueing is a manual UPDATE.
func (p *PostgresStore) FailJobs(ctx context.Context, jobIDs []int64, errMsg string) error {
	if _, err := p.pool.Exec(ctx, `
		UPDATE extraction_jobs SET status = 'failed', last_error = $2, updated_at = now()
		WHERE id = ANY($1)`, jobIDs, truncateErr(errMsg)); err != nil {
		return fmt.Errorf("failing jobs: %w", err)
	}
	return nil
}

// ReclaimStale resets rows stuck in 'processing' (crashed workers, lost
// pods) back to pending. Safe to run against live workers as long as
// reclaimAfter exceeds the longest LLM call.
func (p *PostgresStore) ReclaimStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE extraction_jobs SET status = 'pending', updated_at = now()
		WHERE status = 'processing' AND updated_at < now() - make_interval(secs => $1)`,
		int(olderThan.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("reclaiming stale jobs: %w", err)
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
