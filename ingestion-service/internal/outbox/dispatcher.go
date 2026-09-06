// Package outbox reliably queues READY uploads for transcription. The
// dispatcher claims due events, publishes them to Kafka, and retries with
// capped backoff until the broker accepts. Broker downtime only grows the
// outbox; uploads keep working.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/queue"
	"checkpoint/ingestion/internal/repository"
)

// BatchSize bounds one claim round. Invariant: BatchSize times the Kafka
// publish timeout (5s) must stay comfortably below LockFor, because one
// tick delivers its batch sequentially and every event must still hold
// its lease when its turn comes.
const (
	BatchSize    = 5
	LockFor      = 60 * time.Second
	PollInterval = 2 * time.Second
)

// Publisher publishes one outbox event to Kafka. *queue.FranzProducer
// satisfies it.
type Publisher interface {
	Publish(ctx context.Context, msg queue.Message) error
}

// Dispatcher runs the claim-deliver loop for one process.
type Dispatcher struct {
	store      repository.OutboxRepository
	pub        Publisher
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
	pub Publisher,
	instanceID string,
	log *slog.Logger,
	reg *metrics.Registry,
) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{
		store:      store,
		pub:        pub,
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
// Delivery uses a detached context so SIGTERM drain lets in-flight work
// finish instead of aborting and forcing a 60s lease stall.
func (d *Dispatcher) Tick(ctx context.Context) (int, error) {
	now := d.now()
	events, err := d.store.ClaimDue(ctx, d.instanceID, d.batch, LockFor, now)
	if err != nil {
		return 0, err
	}
	d.refreshBacklogGauges(ctx)
	deliverCtx := context.WithoutCancel(ctx)
	delivered := 0
	for _, ev := range events {
		start := d.now()
		switch d.deliver(deliverCtx, ev) {
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
// climbing past seconds into minutes means Kafka delivery is unhealthy.
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
			if errors.Is(markErr, repository.ErrStaleLease) {
				d.log.Info("outbox lease lost before failed", "event_id", ev.ID, "attempt", ev.Attempt)
				return outcomeStale
			}
			d.log.Error("mark event failed", "event_id", ev.ID, "error", markErr)
		}
		return outcomeFailed
	}

	err := d.pub.Publish(ctx, queue.Message{
		Key: ev.AggregateID,
		Headers: map[string]string{
			"event_id":   payload.EventID,
			"event_type": payload.EventType,
		},
		Value: ev.Payload,
	})
	if err != nil {
		d.log.Warn("kafka publish failed",
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
	if err := d.store.MarkDelivered(ctx, ev.ID, ev.AggregateID, ev.Attempt, now); err != nil {
		if errors.Is(err, repository.ErrStaleLease) {
			d.log.Info("outbox lease lost before delivered", "event_id", ev.ID, "attempt", ev.Attempt)
			return outcomeStale
		}
		d.log.Error("mark delivered failed", "event_id", ev.ID, "error", err)
		return outcomeRetry
	}
	d.log.Info("transcription job queued", "event_id", ev.ID, "upload_id", ev.AggregateID)
	return outcomeDelivered
}
