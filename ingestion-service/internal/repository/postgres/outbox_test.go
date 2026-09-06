package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

func seedReadyEvent(t *testing.T, p *Pool) (uploadID, eventID string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	uploadID = uuid.NewString()
	eventID = uuid.NewString()
	size := int64(64)
	u := &domain.Upload{
		ID: uploadID, UserID: uuid.NewString(), Bucket: "audio",
		ObjectKey: "u/2026/09/" + uploadID, OriginalFilename: "m.wav",
		ContentType: "audio/wav", ExpectedSize: &size,
		Status:    domain.StatusUploading,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := p.Create(context.Background(), u); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := p.MarkReadyAndCreateEvent(context.Background(), completeParams(u)); err != nil {
		t.Fatalf("ready: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = p.inner.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id = $1`, uploadID)
		_, _ = p.inner.Exec(ctx, `DELETE FROM uploads WHERE id = $1`, uploadID)
	})
	return uploadID, eventID
}

func TestClaimDeliverCycle(t *testing.T) {
	p := testPool(t)
	uploadID, _ := seedReadyEvent(t, p)
	now := time.Now().UTC()

	claimed, err := p.ClaimDue(context.Background(), "inst-1", 20, 30*time.Second, now)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].AggregateID != uploadID || claimed[0].Attempt != 1 {
		t.Fatalf("unexpected claim: %+v", claimed)
	}

	// Second claim must not see the leased row.
	again, err := p.ClaimDue(context.Background(), "inst-2", 20, 30*time.Second, now)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("leased row must be invisible, got %d", len(again))
	}

	if err := p.MarkDelivered(context.Background(), claimed[0].ID, uploadID, claimed[0].Attempt, now); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	var outStatus, upStatus string
	_ = p.inner.QueryRow(context.Background(),
		`SELECT status FROM outbox_events WHERE id = $1`, claimed[0].ID).Scan(&outStatus)
	_ = p.inner.QueryRow(context.Background(),
		`SELECT status FROM uploads WHERE id = $1`, uploadID).Scan(&upStatus)
	if outStatus != domain.OutboxDelivered || upStatus != domain.StatusSubmitted {
		t.Fatalf("outbox=%s upload=%s", outStatus, upStatus)
	}
}

func TestScheduleRetryAndExpiryReclaim(t *testing.T) {
	p := testPool(t)
	_, _ = seedReadyEvent(t, p)
	now := time.Now().UTC()

	claimed, err := p.ClaimDue(context.Background(), "inst-1", 20, 30*time.Second, now)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	next := now.Add(domain.NextRetryDelay(claimed[0].Attempt))
	if err := p.ScheduleRetry(context.Background(), claimed[0].ID, claimed[0].Attempt, next, "vad down"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	// Not due yet: nothing claimable.
	early, err := p.ClaimDue(context.Background(), "inst-1", 20, 30*time.Second, now)
	if err != nil || len(early) != 0 {
		t.Fatalf("early claim: %+v %v", early, err)
	}
	// After the backoff passes: claimable again with attempt 2.
	later, err := p.ClaimDue(context.Background(), "inst-2", 20, 30*time.Second, next.Add(time.Second))
	if err != nil || len(later) != 1 || later[0].Attempt != 2 {
		t.Fatalf("later claim: %+v %v", later, err)
	}
}

func TestOutboxStats(t *testing.T) {
	p := testPool(t)
	uploadID, _ := seedReadyEvent(t, p)

	pending, age, err := p.OutboxStats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if pending != 1 || age < 0 {
		t.Fatalf("pending=%d age=%v", pending, age)
	}

	claimed, err := p.ClaimDue(context.Background(), "inst-1", 20, 30*time.Second, time.Now().UTC())
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if err := p.MarkDelivered(context.Background(), claimed[0].ID, uploadID, claimed[0].Attempt, time.Now().UTC()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	pending, _, err = p.OutboxStats(context.Background())
	if err != nil || pending != 0 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
}

// TestStaleLeaseCannotRegressDelivered replays the fencing scenario: a
// slow worker's lease expires, a second worker claims and delivers, and
// the stale worker's late retry/deliver must not move the event.
func TestStaleLeaseCannotRegressDelivered(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	uploadID, _ := seedReadyEvent(t, p)
	now := time.Now().UTC()

	stale, err := p.ClaimDue(ctx, "inst-1", 5, time.Minute, now)
	if err != nil || len(stale) != 1 || stale[0].Attempt != 1 {
		t.Fatalf("claim: %+v %v", stale, err)
	}

	// Lease expires; inst-2 reclaims and delivers as the rightful owner.
	owner, err := p.ClaimDue(ctx, "inst-2", 5, time.Minute, now.Add(2*time.Minute))
	if err != nil || len(owner) != 1 || owner[0].Attempt != 2 {
		t.Fatalf("reclaim: %+v %v", owner, err)
	}
	if err := p.MarkDelivered(ctx, owner[0].ID, uploadID, owner[0].Attempt, now); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	// The stale worker wakes up: its retry must be refused and the
	// DELIVERED state must stand.
	if err := p.ScheduleRetry(ctx, stale[0].ID, stale[0].Attempt, now.Add(time.Minute), "slow vad"); !errors.Is(err, repository.ErrStaleLease) {
		t.Fatalf("stale retry must be refused, got %v", err)
	}
	var status, upStatus string
	_ = p.inner.QueryRow(ctx, `SELECT status FROM outbox_events WHERE id = $1`, stale[0].ID).Scan(&status)
	_ = p.inner.QueryRow(ctx, `SELECT status FROM uploads WHERE id = $1`, uploadID).Scan(&upStatus)
	if status != domain.OutboxDelivered || upStatus != domain.StatusSubmitted {
		t.Fatalf("stale retry regressed state: outbox=%s upload=%s", status, upStatus)
	}
}

// TestStaleLeaseCannotDeliver verifies a stale MarkDelivered touches
// neither the event nor the upload.
func TestStaleLeaseCannotDeliver(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	uploadID, _ := seedReadyEvent(t, p)
	now := time.Now().UTC()

	stale, err := p.ClaimDue(ctx, "inst-1", 5, time.Minute, now)
	if err != nil || len(stale) != 1 {
		t.Fatalf("claim: %+v %v", stale, err)
	}
	owner, err := p.ClaimDue(ctx, "inst-2", 5, time.Minute, now.Add(2*time.Minute))
	if err != nil || len(owner) != 1 {
		t.Fatalf("reclaim: %+v %v", owner, err)
	}

	if err := p.MarkDelivered(ctx, stale[0].ID, uploadID, stale[0].Attempt, now); !errors.Is(err, repository.ErrStaleLease) {
		t.Fatalf("stale deliver must be refused, got %v", err)
	}
	var status, upStatus string
	_ = p.inner.QueryRow(ctx, `SELECT status FROM outbox_events WHERE id = $1`, stale[0].ID).Scan(&status)
	_ = p.inner.QueryRow(ctx, `SELECT status FROM uploads WHERE id = $1`, uploadID).Scan(&upStatus)
	if status != domain.OutboxProcessing || upStatus != domain.StatusReady {
		t.Fatalf("stale deliver moved state: outbox=%s upload=%s", status, upStatus)
	}
}
