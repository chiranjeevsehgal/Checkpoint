package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"notification-service/internal/model"
)

// PostgresStore reads due reminders and records notification deliveries. It
// connects as checkpoint_worker, which already has SELECT on reminders and
// user_notification_settings and DML on notification_deliveries.
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

// missingTableWarned keeps the "source tables missing" warning to one line per
// process instead of one per poll.
var missingTableWarned sync.Once

// isMissingTable reports whether err is Postgres' undefined_table (42P01),
// which happens in split deployments without the ingestion tables.
func (p *PostgresStore) isMissingTable(err error) bool {
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		return false
	}
	missingTableWarned.Do(func() {
		log.Printf("reminder tables missing; nothing to notify")
	})
	return true
}

// DueCandidates returns reminders whose advance or due fire time has arrived,
// joined to the owner's enabled channel. Tombstoned users are skipped.
func (p *PostgresStore) DueCandidates(ctx context.Context, kind string, lead, grace, maxLateness time.Duration, limit int) ([]model.Candidate, error) {
	candidates, err := p.dueCandidates(ctx, true, kind, lead, grace, maxLateness, limit)
	if err != nil && p.isMissingTable(err) {
		candidates, err = p.dueCandidates(ctx, false, kind, lead, grace, maxLateness, limit)
	}
	if err != nil {
		if p.isMissingTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("querying %s candidates: %w", kind, err)
	}
	return candidates, nil
}

// dueCandidates runs DueCandidates' query; withTombstone joins account_deletions
// to skip deleted accounts.
func (p *PostgresStore) dueCandidates(ctx context.Context, withTombstone bool, kind string, lead, grace, maxLateness time.Duration, limit int) ([]model.Candidate, error) {
	var fireExpr, window string
	var args []interface{}
	switch kind {
	case model.KindAdvance:
		fireExpr = `r.remind_at - make_interval(secs => $1)`
		window = `r.remind_at > now()
		  AND r.remind_at <= now() + make_interval(secs => $1)
		  AND r.remind_at >= now() + make_interval(secs => $1 - $2)`
		args = []interface{}{int(lead.Seconds()), int(grace.Seconds())}
	case model.KindDue:
		fireExpr = `r.remind_at`
		window = `r.remind_at <= now()
		  AND r.remind_at >= now() - make_interval(secs => $1)`
		args = []interface{}{int(maxLateness.Seconds())}
	default:
		return nil, fmt.Errorf("unknown delivery kind %q", kind)
	}
	limitIndex := len(args) + 1
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT r.user_id::text, r.audio_id::text, r.text, s.ntfy_topic, %s
		FROM reminders r
		JOIN user_notification_settings s
		  ON s.user_id = r.user_id::uuid AND s.enabled AND s.ntfy_topic IS NOT NULL
		WHERE r.remind_at IS NOT NULL
		  AND %s`, fireExpr, window)
	if withTombstone {
		query += `
		  AND NOT EXISTS (SELECT 1 FROM account_deletions d WHERE d.user_id = r.user_id::uuid)`
	}
	query += fmt.Sprintf(`
		ORDER BY r.remind_at
		LIMIT $%d`, limitIndex)

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []model.Candidate
	for rows.Next() {
		var c model.Candidate
		if err := rows.Scan(&c.UserID, &c.AudioID, &c.ReminderText, &c.Topic, &c.FireAt); err != nil {
			return nil, err
		}
		c.Kind = kind
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// ClaimDelivery atomically inserts or re-claims a delivery. It returns
// claimed=false when the delivery was already sent or is still in-flight.
func (p *PostgresStore) ClaimDelivery(ctx context.Context, c model.Candidate, maxAttempts int, reclaimAfter time.Duration) (int64, int, bool, error) {
	var id int64
	var attempts int
	err := p.pool.QueryRow(ctx, `
		INSERT INTO notification_deliveries
			(user_id, audio_id, reminder_text, kind, fire_at, status, attempts)
		VALUES ($1, $2, $3, $4, $5, 'processing', 1)
		ON CONFLICT (user_id, audio_id, reminder_text, fire_at, kind) DO UPDATE
			SET status = 'processing',
			    attempts = notification_deliveries.attempts + 1,
			    updated_at = now()
			WHERE notification_deliveries.attempts < $6
			  AND (notification_deliveries.status = 'failed'
			       OR (notification_deliveries.status = 'processing'
			           AND notification_deliveries.updated_at <= now() - make_interval(secs => $7)))
		RETURNING id, attempts`,
		c.UserID, c.AudioID, c.ReminderText, c.Kind, c.FireAt, maxAttempts, int(reclaimAfter.Seconds())).
		Scan(&id, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("claiming delivery: %w", err)
	}
	return id, attempts, true, nil
}

// MarkSent records a successful publish.
func (p *PostgresStore) MarkSent(ctx context.Context, id int64) error {
	if _, err := p.pool.Exec(ctx, `
		UPDATE notification_deliveries
		SET status = 'sent', sent_at = now(), updated_at = now(), last_error = NULL
		WHERE id = $1`, id); err != nil {
		return fmt.Errorf("marking delivery %d sent: %w", id, err)
	}
	return nil
}

// MarkFailed records a failed publish so the next poll can retry it.
func (p *PostgresStore) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	if _, err := p.pool.Exec(ctx, `
		UPDATE notification_deliveries
		SET status = 'failed', last_error = $2, updated_at = now()
		WHERE id = $1`, id, truncateErr(errMsg)); err != nil {
		return fmt.Errorf("marking delivery %d failed: %w", id, err)
	}
	return nil
}

// PruneOld deletes delivery rows older than the retention window.
func (p *PostgresStore) PruneOld(ctx context.Context, retention time.Duration) (int64, error) {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM notification_deliveries
		WHERE updated_at < now() - make_interval(secs => $1)`, int(retention.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("pruning deliveries: %w", err)
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
