package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"checkpoint/ingestion/internal/repository"
)

// ClaimDue leases up to batch due events to one dispatcher instance.
// SKIP LOCKED lets multiple replicas claim without blocking each other,
// and expired PROCESSING locks are reclaimed so crashed dispatchers
// cannot strand events.
func (p *Pool) ClaimDue(ctx context.Context, instanceID string, batch int, lockFor time.Duration, now time.Time) ([]repository.ClaimedEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := p.inner.Query(ctx, `
		WITH claimed AS (
			SELECT id FROM outbox_events
			WHERE status IN ('PENDING', 'PROCESSING')
			  AND next_attempt_at <= $1
			  AND (locked_until IS NULL OR locked_until <= $1)
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events e
		SET status = 'PROCESSING',
			locked_by = $3,
			locked_until = $4,
			attempt_count = e.attempt_count + 1,
			last_error = NULL
		FROM claimed WHERE e.id = claimed.id
		RETURNING e.id, e.aggregate_id, e.event_type, e.payload, e.attempt_count`,
		now, batch, instanceID, now.Add(lockFor))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (repository.ClaimedEvent, error) {
		var ev repository.ClaimedEvent
		err := row.Scan(&ev.ID, &ev.AggregateID, &ev.EventType, &ev.Payload, &ev.Attempt)
		return ev, err
	})
}

// MarkDelivered records VAD acceptance and flips the upload to
// SUBMITTED in one transaction, so the two can never disagree. The
// outbox update is fenced on the claimed attempt: a worker whose lease
// expired reports ErrStaleLease and touches nothing.
func (p *Pool) MarkDelivered(ctx context.Context, eventID, uploadID string, attempt int, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE outbox_events
		SET status = 'DELIVERED', delivered_at = $1, locked_by = NULL, locked_until = NULL
		WHERE id = $2 AND status = 'PROCESSING' AND attempt_count = $3`, now, eventID, attempt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrStaleLease
	}
	tag, err = tx.Exec(ctx, `
		UPDATE uploads SET status = 'SUBMITTED', submitted_at = $1, updated_at = $1
		WHERE id = $2 AND status = 'READY'`, now, uploadID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrStaleLease
	}
	return tx.Commit(ctx)
}

// ScheduleRetry returns the event to PENDING with a backoff deadline.
// Fenced on the claimed attempt like MarkDelivered: a stale worker
// must not resurrect another owner's DELIVERED event.
func (p *Pool) ScheduleRetry(ctx context.Context, eventID string, attempt int, nextAttempt time.Time, errMsg string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tag, err := p.inner.Exec(ctx, `
		UPDATE outbox_events
		SET status = 'PENDING', next_attempt_at = $1, last_error = $2,
			locked_by = NULL, locked_until = NULL
		WHERE id = $3 AND status = 'PROCESSING' AND attempt_count = $4`,
		nextAttempt, errMsg, eventID, attempt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrStaleLease
	}
	return nil
}

// OutboxStats reports the dispatcher backlog: how many events await
// delivery and how old the oldest one is. The age is the key pipeline
// health signal: it stays near zero and climbs when VAD delivery stalls.
func (p *Pool) OutboxStats(ctx context.Context) (pending int64, oldestAge time.Duration, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var ageSeconds float64
	err = p.inner.QueryRow(ctx, `
		SELECT COUNT(*),
			COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(created_at))), 0)
		FROM outbox_events
		WHERE status IN ('PENDING', 'PROCESSING')`).Scan(&pending, &ageSeconds)
	if err != nil {
		return 0, 0, err
	}
	return pending, time.Duration(ageSeconds * float64(time.Second)), nil
}

// MarkFailed parks an event for operational intervention. Reserved for
// poison events that can never be delivered, not transient outages.
// Fenced to PENDING/PROCESSING so a stale worker cannot regress a
// DELIVERED event.
func (p *Pool) MarkFailed(ctx context.Context, eventID, errMsg string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tag, err := p.inner.Exec(ctx, `
		UPDATE outbox_events
		SET status = 'FAILED', last_error = $1, delivered_at = NULL,
			locked_by = NULL, locked_until = NULL
		WHERE id = $2 AND status IN ('PENDING', 'PROCESSING')`, errMsg, eventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrStaleLease
	}
	return nil
}
