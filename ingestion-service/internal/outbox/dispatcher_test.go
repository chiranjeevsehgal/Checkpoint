package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/vadclient"
)

type fakeStore struct {
	claimed     []repository.ClaimedEvent
	delivered   []string
	deliverUpID []string
	retried     map[string]time.Time
	failed      []string
	deliverErr  error
}

func (f *fakeStore) ClaimDue(_ context.Context, _ string, _ int, _ time.Duration, _ time.Time) ([]repository.ClaimedEvent, error) {
	return f.claimed, nil
}

func (f *fakeStore) MarkDelivered(_ context.Context, eventID, uploadID string, _ int, _ time.Time) error {
	if f.deliverErr != nil {
		return f.deliverErr
	}
	f.delivered = append(f.delivered, eventID)
	f.deliverUpID = append(f.deliverUpID, uploadID)
	return nil
}

func (f *fakeStore) ScheduleRetry(_ context.Context, eventID string, _ int, next time.Time, _ string) error {
	if f.retried == nil {
		f.retried = map[string]time.Time{}
	}
	f.retried[eventID] = next
	return nil
}

func (f *fakeStore) MarkFailed(_ context.Context, eventID, _ string, _ time.Time) error {
	f.failed = append(f.failed, eventID)
	return nil
}

func (f *fakeStore) OutboxStats(_ context.Context) (int64, time.Duration, error) {
	return int64(len(f.claimed)), 0, nil
}

type fakeVAD struct {
	err   error
	calls int
}

func (f *fakeVAD) SubmitJob(_ context.Context, _ vadclient.JobRequest) error {
	f.calls++
	return f.err
}

func testPayload(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(domain.AudioReadyPayload{
		SchemaVersion: 1,
		EventID:       "event-1",
		EventType:     domain.EventAudioReadyForVAD,
		Data: domain.AudioReadyData{
			AudioID: "audio-1", Bucket: "audio",
			ObjectKey: "u/2026/09/audio-1", ContentType: "audio/wav", SizeBytes: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTickDelivers(t *testing.T) {
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-1", AggregateID: "audio-1", Payload: testPayload(t), Attempt: 1},
	}}
	d := NewDispatcher(store, &fakeVAD{}, "test-1", nil, metrics.NewRegistry())
	n, err := d.Tick(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(store.delivered) != 1 || len(store.retried) != 0 {
		t.Fatalf("delivered=%v retried=%v", store.delivered, store.retried)
	}
}

func TestTickUsesAggregateID(t *testing.T) {
	raw, err := json.Marshal(domain.AudioReadyPayload{
		SchemaVersion: 1,
		EventID:       "event-9",
		EventType:     domain.EventAudioReadyForVAD,
		Data: domain.AudioReadyData{
			AudioID: "payload-audio", Bucket: "audio",
			ObjectKey: "u/2026/09/x", ContentType: "audio/ogg", SizeBytes: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-9", AggregateID: "agg-9", Payload: raw, Attempt: 1},
	}}
	d := NewDispatcher(store, &fakeVAD{}, "test-1", nil, metrics.NewRegistry())
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.deliverUpID) != 1 || store.deliverUpID[0] != "agg-9" {
		t.Fatalf("MarkDelivered must use AggregateID, got %v", store.deliverUpID)
	}
}

func TestTickRetriesWithBackoff(t *testing.T) {
	before := time.Now().UTC()
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-1", AggregateID: "audio-1", Payload: testPayload(t), Attempt: 2},
	}}
	d := NewDispatcher(store, &fakeVAD{err: errors.New("vad down")}, "test-1", nil, metrics.NewRegistry())
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatalf("tick must not fail on delivery error: %v", err)
	}
	next, ok := store.retried["event-1"]
	if !ok || len(store.delivered) != 0 {
		t.Fatalf("must schedule retry, got %v %v", store.delivered, store.retried)
	}
	if next.Before(before.Add(15*time.Second)) || next.After(before.Add(60*time.Second)) {
		t.Fatalf("attempt 2 must back off ~15s, got %v", next.Sub(before))
	}
}

func TestTickPoisonPayloadFails(t *testing.T) {
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-1", AggregateID: "audio-1", Payload: []byte("{bad"), Attempt: 1},
	}}
	d := NewDispatcher(store, &fakeVAD{}, "test-1", nil, metrics.NewRegistry())
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatalf("tick must not fail on poison event: %v", err)
	}
	if len(store.failed) != 1 || len(store.delivered) != 0 {
		t.Fatalf("poison event must be FAILED, got failed=%v delivered=%v", store.failed, store.retried)
	}
}

// TestTickStaleLeaseIsQuiet proves a worker that lost its lease neither
// counts a delivery nor schedules a duplicate retry: the rightful owner
// already acted.
func TestTickStaleLeaseIsQuiet(t *testing.T) {
	store := &fakeStore{
		claimed: []repository.ClaimedEvent{
			{ID: "event-1", AggregateID: "audio-1", Payload: testPayload(t), Attempt: 1},
		},
		deliverErr: repository.ErrStaleLease,
	}
	d := NewDispatcher(store, &fakeVAD{}, "test-1", nil, metrics.NewRegistry())
	n, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("stale lease must not fail the tick: %v", err)
	}
	if n != 0 || len(store.delivered) != 0 || len(store.retried) != 0 {
		t.Fatalf("stale event must be dropped quietly, got n=%d delivered=%v retried=%v",
			n, store.delivered, store.retried)
	}
}
