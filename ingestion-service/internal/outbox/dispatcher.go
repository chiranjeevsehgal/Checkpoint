// Package outbox reliably hands READY uploads to VAD. The dispatcher
// claims due events, submits them, and retries with capped backoff until
// VAD accepts. VAD downtime only grows the outbox; uploads keep working.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/domain"
	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/metrics"
	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/repository"
	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/vadclient"
)

// BatchSize bounds one claim round. Invariant: BatchSize times the VAD
// request timeout (5s) must stay comfortably below LockFor, because one
// tick delivers its batch sequentially and every event must still hold
// its lease when its turn comes.
const (
	BatchSize    = 5
	LockFor      = 60 * time.Second
	PollInterval = 2 * time.Second
)

// Submitter delivers one job to VAD. *vadclient.Client satisfies it.
type Submitter interface {
	SubmitJob(ctx context.Context, req vadclient.JobRequest) error
}

// Dispatcher runs the claim-deliver loop for one process.
type Dispatcher struct {
	store      repository.OutboxRepository
	vad        Submitter
	instanceID string
	batch      int
	poll       time.Duration
	now        func() time.Time
	log        *slog.Logger
	deliveries *metrics.Counter
	duration   *metrics.Histogram
	pending    *metrics.Gauge
	oldestAge  *metrics.Gauge
}

// NewDispatcher wires a dispatcher. Pass nil logger for a default one.
func NewDispatcher(
	store repository.OutboxRepository,
	vad Submitter,
	instanceID string,
	log *slog.Logger,
	reg *metrics.Registry,
) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{
		store:      store,
		vad:        vad,
		instanceID: instanceID,
		batch:      BatchSize,
		poll:       PollInterval,
		now:        func() time.Time { return time.Now().UTC() },
		log:        log,
		deliveries: reg.Counter("outbox_delivery_total", "result"),
		duration:   reg.Histogram("outbox_delivery_duration_seconds", metrics.DefaultLatencyBuckets),
		pending:    reg.Gauge("outbox_pending_total"),
		oldestAge:  reg.Gauge("oldest_outbox_event_age_seconds"),
	}
}

// Run claims and delivers until ctx is done. Transient errors are logged
// and retried next tick; Run only returns on shutdown.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			d.log.Info("outbox dispatcher stopping")
			return
		case <-ticker.C:
			if _, err := d.Tick(ctx); err != nil {
				d.log.Error("outbox tick failed", "error", err)
			}
		}
	}
}

// Tick claims one batch and delivers it, returning the delivered count.
func (d *Dispatcher) Tick(ctx context.Context) (int, error) {
	now := d.now()
	events, err := d.store.ClaimDue(ctx, d.instanceID, d.batch, LockFor, now)
	if err != nil {
		return 0, err
	}
	d.refreshBacklogGauges(ctx)
	delivered := 0
	for _, ev := range events {
		start := d.now()
		switch d.deliver(ctx, ev) {
		case outcomeDelivered:
			d.deliveries.Inc("delivered")
			delivered++
		case outcomeRetry:
			d.deliveries.Inc("retry")
		case outcomeStale:
			d.deliveries.Inc("stale")
		case outcomeFailed:
			d.deliveries.Inc("failed")
		}
		d.duration.Observe(d.now().Sub(start).Seconds())
	}
	return delivered, nil
}

// refreshBacklogGauges publishes queue depth and head-of-line age. Age
// climbing past seconds into minutes means VAD delivery is unhealthy.
func (d *Dispatcher) refreshBacklogGauges(ctx context.Context) {
	pending, age, err := d.store.OutboxStats(ctx)
	if err != nil {
		d.log.Warn("outbox stats failed", "error", err)
		return
	}
	d.pending.Set(float64(pending))
	d.oldestAge.Set(age.Seconds())
}

// outcome classifies one delivery attempt for metrics.
type outcome int

const (
	outcomeDelivered outcome = iota
	outcomeRetry
	outcomeStale
	outcomeFailed
)

func (d *Dispatcher) deliver(ctx context.Context, ev repository.ClaimedEvent) outcome {
	now := d.now()

	var payload domain.AudioReadyPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		// We wrote this payload, so corruption means manual inspection.
		if markErr := d.store.MarkFailed(ctx, ev.ID, "unmarshal payload: "+err.Error(), now); markErr != nil {
			d.log.Error("mark event failed", "event_id", ev.ID, "error", markErr)
		}
		return outcomeFailed
	}

	err := d.vad.SubmitJob(ctx, vadclient.JobRequest{
		EventID:     payload.EventID,
		AudioID:     payload.Data.AudioID,
		Bucket:      payload.Data.Bucket,
		ObjectKey:   payload.Data.ObjectKey,
		ContentType: payload.Data.ContentType,
		SizeBytes:   payload.Data.SizeBytes,
	})
	if err != nil {
		d.log.Warn("vad submission failed",
			"event_id", ev.ID, "upload_id", ev.AggregateID,
			"attempt", ev.Attempt, "error", err)
		next := now.Add(domain.NextRetryDelay(ev.Attempt))
		if retryErr := d.store.ScheduleRetry(ctx, ev.ID, ev.Attempt, next, err.Error()); retryErr != nil {
			if errors.Is(retryErr, repository.ErrStaleLease) {
				d.log.Info("outbox lease lost before retry", "event_id", ev.ID, "attempt", ev.Attempt)
				return outcomeStale
			}
			d.log.Error("schedule retry failed", "event_id", ev.ID, "error", retryErr)
		}
		return outcomeRetry
	}
	if err := d.store.MarkDelivered(ctx, ev.ID, payload.Data.AudioID, ev.Attempt, now); err != nil {
		if errors.Is(err, repository.ErrStaleLease) {
			d.log.Info("outbox lease lost before delivered", "event_id", ev.ID, "attempt", ev.Attempt)
			return outcomeStale
		}
		d.log.Error("mark delivered failed", "event_id", ev.ID, "error", err)
		return outcomeRetry
	}
	d.log.Info("vad job accepted", "event_id", ev.ID, "upload_id", ev.AggregateID)
	return outcomeDelivered
}
