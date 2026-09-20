package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"rollup-service/internal/model"
)

// PostgresStore reads the pipeline's tables and upserts summary narratives.
// The worker connects as checkpoint_worker, which already has SELECT on
// transcripts/user_settings and DML on summaries.
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
// process instead of one per query.
var missingTableWarned sync.Once

// isMissingTable reports whether err is Postgres' undefined_table (42P01),
// which happens in split deployments without the ingestion tables.
func (p *PostgresStore) isMissingTable(err error) bool {
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		return false
	}
	missingTableWarned.Do(func() {
		slog.Warn("source tables missing; nothing to summarize")
	})
	return true
}

// IsUserDeleting reports whether a deletion tombstone exists for the user.
// A missing table means there is nothing to gate on.
func (p *PostgresStore) IsUserDeleting(ctx context.Context, userID string) (bool, error) {
	var deleting bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM account_deletions WHERE user_id = $1::uuid)`, userID).Scan(&deleting)
	if err != nil {
		if p.isMissingTable(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking tombstone for %s: %w", userID, err)
	}
	return deleting, nil
}

// ActiveUsers returns users with a transcript recorded since `since`, plus
// their stored IANA zone ("" when unset). No transcripts means no summaries.
func (p *PostgresStore) ActiveUsers(ctx context.Context, since time.Time) ([]model.UserRef, error) {
	users, err := p.activeUsers(ctx, true, since)
	if err != nil && p.isMissingTable(err) {
		users, err = p.activeUsers(ctx, false, since)
	}
	if err != nil {
		if p.isMissingTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing active users: %w", err)
	}
	return users, nil
}

func (p *PostgresStore) activeUsers(ctx context.Context, withSettings bool, since time.Time) ([]model.UserRef, error) {
	query := `SELECT DISTINCT t.user_id::text, `
	if withSettings {
		query += `s.timezone`
	} else {
		query += `NULL::text`
	}
	query += `
		FROM transcripts t`
	if withSettings {
		query += `
		LEFT JOIN user_settings s ON s.user_id = t.user_id`
	}
	query += `
		WHERE coalesce(t.recorded_at, t.created_at) > $1`

	rows, err := p.pool.Query(ctx, query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.UserRef
	for rows.Next() {
		var user model.UserRef
		var timezone *string
		if err := rows.Scan(&user.UserID, &timezone); err != nil {
			return nil, err
		}
		if timezone != nil {
			user.Timezone = *timezone
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// DaySources loads one user's transcripts and extracted items for a local day.
func (p *PostgresStore) DaySources(ctx context.Context, userID string, loc *time.Location, day time.Time) (model.DaySources, error) {
	zone := loc.String()
	date := day.Format("2006-01-02")
	var src model.DaySources
	var err error

	if src.Transcripts, err = p.queryStrings(ctx, `
		SELECT text FROM transcripts
		WHERE user_id = $1::uuid
		  AND (coalesce(recorded_at, created_at) AT TIME ZONE $2)::date = $3::date
		ORDER BY coalesce(recorded_at, created_at)`, userID, zone, date); err != nil {
		return src, fmt.Errorf("loading transcripts for %s: %w", userID, err)
	}
	if src.Todos, err = p.queryStrings(ctx, `
		SELECT td.text FROM todos td
		JOIN transcripts tr ON tr.audio_id = td.audio_id::uuid
		WHERE td.user_id = $1
		  AND (coalesce(tr.recorded_at, tr.created_at) AT TIME ZONE $2)::date = $3::date
		ORDER BY td.created_at`, userID, zone, date); err != nil {
		return src, fmt.Errorf("loading todos for %s: %w", userID, err)
	}
	if src.Reminders, err = p.queryStrings(ctx, `
		SELECT rm.text FROM reminders rm
		JOIN transcripts tr ON tr.audio_id = rm.audio_id::uuid
		WHERE rm.user_id = $1
		  AND (coalesce(tr.recorded_at, tr.created_at) AT TIME ZONE $2)::date = $3::date
		ORDER BY rm.created_at`, userID, zone, date); err != nil {
		return src, fmt.Errorf("loading reminders for %s: %w", userID, err)
	}
	if src.Insights, err = p.queryStrings(ctx, `
		SELECT ins.text FROM insights ins
		JOIN transcripts tr ON tr.audio_id = ins.audio_id::uuid
		WHERE ins.user_id = $1
		  AND (coalesce(tr.recorded_at, tr.created_at) AT TIME ZONE $2)::date = $3::date
		ORDER BY ins.created_at`, userID, zone, date); err != nil {
		return src, fmt.Errorf("loading insights for %s: %w", userID, err)
	}
	return src, nil
}

func (p *PostgresStore) queryStrings(ctx context.Context, query string, args ...interface{}) ([]string, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// Summaries returns stored narratives for a user, period and inclusive
// period_start range, oldest first.
func (p *PostgresStore) Summaries(ctx context.Context, userID, period string, from, to time.Time) ([]model.Summary, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT user_id, period, period_start, text, model
		FROM summaries
		WHERE user_id = $1 AND period = $2 AND period_start BETWEEN $3 AND $4
		ORDER BY period_start`, userID, period, from, to)
	if err != nil {
		return nil, fmt.Errorf("loading summaries for %s: %w", userID, err)
	}
	defer rows.Close()

	var summaries []model.Summary
	for rows.Next() {
		var s model.Summary
		if err := rows.Scan(&s.UserID, &s.Period, &s.PeriodStart, &s.Text, &s.Model); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// UpsertSummary writes a narrative, replacing any existing row for the period.
func (p *PostgresStore) UpsertSummary(ctx context.Context, s model.Summary) error {
	if _, err := p.pool.Exec(ctx, `
		INSERT INTO summaries (user_id, period, period_start, text, model)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, period, period_start)
		DO UPDATE SET text = EXCLUDED.text, model = EXCLUDED.model, updated_at = now()`,
		s.UserID, s.Period, s.PeriodStart, s.Text, s.Model); err != nil {
		return fmt.Errorf("upserting %s summary for %s: %w", s.Period, s.UserID, err)
	}
	return nil
}
