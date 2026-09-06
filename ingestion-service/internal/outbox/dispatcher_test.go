package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/queue"
	"checkpoint/ingestion/internal/repository"
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

type fakePublisher struct {
	err   error
	calls int
	msgs  []queue.Message
}

func (f *fakePublisher) Publish(_ context.Context, msg queue.Message) error {
	f.calls++
	if f.err == nil {
		f.msgs = append(f.msgs, msg)
	}
	return f.err
}

func testPayload(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(domain.AudioReadyPayload{
		SchemaVersion: domain.SchemaVersion,
		EventID:       "event-1",
		EventType:     domain.EventTranscriptionRequested,
		Data: domain.AudioReadyData{
			AudioID: "audio-1", UserID: "user-1", Bucket: "audio",
			ObjectKey: "u/2026/09/06/audio-1", ContentType: "audio/ogg", SizeBytes: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTickDelivers(t *testing.T) {
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-1", AggregateID: "audio-1", EventType: domain.EventTranscriptionRequested, Payload: testPayload(t), Attempt: 1},
	}}
	pub := &fakePublisher{}
	d := NewDispatcher(store, pub, "test-1", nil, metrics.NewRegistry())
	n, err := d.Tick(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(store.delivered) != 1 || len(store.retried) != 0 {
		t.Fatalf("delivered=%v retried=%v", store.delivered, store.retried)
	}
	if len(pub.msgs) != 1 || pub.msgs[0].Key != "audio-1" {
		t.Fatalf("must publish keyed by AggregateID, got %+v", pub.msgs)
	}
	if len(pub.msgs[0].Value) == 0 {
		t.Fatal("published value must carry the stored outbox payload")
	}
	if pub.msgs[0].Headers["event_id"] != "event-1" || pub.msgs[0].Headers["event_type"] != domain.EventTranscriptionRequested {
		t.Fatalf("headers must use canonical IDs, got %+v", pub.msgs[0].Headers)
	}
}

func TestTickUsesAggregateID(t *testing.T) {
	raw, err := json.Marshal(domain.AudioReadyPayload{
		SchemaVersion: domain.SchemaVersion,
		EventID:       "event-9",
		EventType:     domain.EventTranscriptionRequested,
		Data: domain.AudioReadyData{
			AudioID: "payload-audio", UserID: "user-1", Bucket: "audio",
			ObjectKey: "u/2026/09/06/x", ContentType: "audio/ogg", SizeBytes: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-9", AggregateID: "agg-9", EventType: domain.EventTranscriptionRequested, Payload: raw, Attempt: 1},
	}}
	pub := &fakePublisher{}
	d := NewDispatcher(store, pub, "test-1", nil, metrics.NewRegistry())
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.deliverUpID) != 1 || store.deliverUpID[0] != "agg-9" {
		t.Fatalf("MarkDelivered must use AggregateID, got %v", store.deliverUpID)
	}
	if len(pub.msgs) != 1 || pub.msgs[0].Key != "agg-9" {
		t.Fatalf("Kafka key must use AggregateID, got %+v", pub.msgs)
	}
}

func TestTickRetriesWithBackoff(t *testing.T) {
	before := time.Now().UTC()
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-1", AggregateID: "audio-1", EventType: domain.EventTranscriptionRequested, Payload: testPayload(t), Attempt: 2},
	}}
	d := NewDispatcher(store, &fakePublisher{err: errors.New("kafka down")}, "test-1", nil, metrics.NewRegistry())
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
		{ID: "event-1", AggregateID: "audio-1", EventType: domain.EventTranscriptionRequested, Payload: []byte("{bad"), Attempt: 1},
	}}
	pub := &fakePublisher{}
	d := NewDispatcher(store, pub, "test-1", nil, metrics.NewRegistry())
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatalf("tick must not fail on poison event: %v", err)
	}
	if len(store.failed) != 1 || len(store.delivered) != 0 {
		t.Fatalf("poison event must be FAILED, got failed=%v delivered=%v", store.failed, store.retried)
	}
	if pub.calls != 0 {
		t.Fatal("poison payload must never reach Kafka")
	}
}

// TestTickStaleLeaseIsQuiet proves a worker that lost its lease neither
// counts a delivery nor schedules a duplicate retry: the rightful owner
// already acted.
func TestTickStaleLeaseIsQuiet(t *testing.T) {
	store := &fakeStore{
		claimed: []repository.ClaimedEvent{
			{ID: "event-1", AggregateID: "audio-1", EventType: domain.EventTranscriptionRequested, Payload: testPayload(t), Attempt: 1},
		},
		deliverErr: repository.ErrStaleLease,
	}
	d := NewDispatcher(store, &fakePublisher{}, "test-1", nil, metrics.NewRegistry())
	n, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("stale lease must not fail the tick: %v", err)
	}
	if n != 0 || len(store.delivered) != 0 || len(store.retried) != 0 {
		t.Fatalf("stale event must be dropped quietly, got n=%d delivered=%v retried=%v",
			n, store.delivered, store.retried)
	}
}

func TestTickRejectsV1Payload(t *testing.T) {
	raw, err := json.Marshal(domain.AudioReadyPayload{
		SchemaVersion: 1,
		EventID:       "event-old",
		EventType:     domain.EventAudioReadyForVAD,
		Data: domain.AudioReadyData{
			AudioID: "audio-1", Bucket: "audio",
			ObjectKey: "u/2026/09/06/audio-1", ContentType: "audio/ogg", SizeBytes: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-old", AggregateID: "audio-1", EventType: domain.EventAudioReadyForVAD, Payload: raw, Attempt: 1},
	}}
	pub := &fakePublisher{}
	d := NewDispatcher(store, pub, "test-1", nil, metrics.NewRegistry())
	n, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick must not fail on stale schema: %v", err)
	}
	if n != 0 || len(store.delivered) != 0 {
		t.Fatalf("v1 payload must not count as delivered, got n=%d delivered=%v", n, store.delivered)
	}
	if len(store.failed) != 1 {
		t.Fatalf("v1 payload must be FAILED for inspection, got failed=%v", store.failed)
	}
	if pub.calls != 0 {
		t.Fatal("v1 payload must never reach Kafka")
	}
}

func TestTickHeadersUseCanonicalIDs(t *testing.T) {
	raw, err := json.Marshal(domain.AudioReadyPayload{
		SchemaVersion: domain.SchemaVersion,
		EventID:       "payload-event-different",
		EventType:     domain.EventTranscriptionRequested,
		Data: domain.AudioReadyData{
			AudioID: "audio-1", UserID: "user-1", Bucket: "audio",
			ObjectKey: "u/2026/09/06/audio-1", ContentType: "audio/ogg", SizeBytes: 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{claimed: []repository.ClaimedEvent{
		{ID: "event-canonical", AggregateID: "audio-1", EventType: domain.EventTranscriptionRequested, Payload: raw, Attempt: 1},
	}}
	pub := &fakePublisher{}
	d := NewDispatcher(store, pub, "test-1", nil, metrics.NewRegistry())
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.msgs) != 1 {
		t.Fatalf("want 1 publish, got %+v", pub.msgs)
	}
	if pub.msgs[0].Headers["event_id"] != "event-canonical" {
		t.Fatalf("event_id header must use row ID, got %+v", pub.msgs[0].Headers)
	}
	if pub.msgs[0].Headers["event_type"] != domain.EventTranscriptionRequested {
		t.Fatalf("event_type header must use row type, got %+v", pub.msgs[0].Headers)
	}
}

func TestTickDuplicateOnMarkDeliveredError(t *testing.T) {
	store := &fakeStore{
		claimed: []repository.ClaimedEvent{
			{ID: "event-1", AggregateID: "audio-1", EventType: domain.EventTranscriptionRequested, Payload: testPayload(t), Attempt: 1},
		},
		deliverErr: errors.New("db down"),
	}
	pub := &fakePublisher{}
	d := NewDispatcher(store, pub, "test-1", nil, metrics.NewRegistry())
	n, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick must not fail on DB error: %v", err)
	}
	if n != 0 {
		t.Fatalf("failed MarkDelivered must not count, got n=%d", n)
	}
	if pub.calls != 1 {
		t.Fatalf("Kafka publish already happened before DB failure, calls=%d", pub.calls)
	}
	if len(store.delivered) != 0 || len(store.failed) != 0 {
		t.Fatalf("must neither deliver nor fail, got delivered=%v failed=%v", store.delivered, store.failed)
	}
}
