package postgres

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/domain"
	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/repository"
)

func testPool(t *testing.T) *Pool {
	t.Helper()
	url := testDatabaseURL()
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	pool, err := NewPool(context.Background(), url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedUploading(t *testing.T, p *Pool, userID string) *domain.Upload {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	size := int64(1024)
	u := &domain.Upload{
		ID:               uuid.NewString(),
		UserID:           userID,
		Bucket:           "audio",
		ObjectKey:        domain.ObjectKeyFor(userID, uuid.NewString(), now),
		OriginalFilename: "meeting.wav",
		ContentType:      "audio/wav",
		ExpectedSize:     &size,
		Status:           domain.StatusUploading,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := p.Create(context.Background(), u); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = p.inner.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id = $1`, u.ID)
		_, _ = p.inner.Exec(ctx, `DELETE FROM uploads WHERE id = $1`, u.ID)
	})
	return u
}

func completeParams(u *domain.Upload) repository.CompleteParams {
	eventID := uuid.NewString()
	payload, _ := json.Marshal(domain.NewAudioReadyPayload(eventID, u, 1024, time.Now().UTC()))
	return repository.CompleteParams{
		UploadID:   u.ID,
		UserID:     u.UserID,
		ActualSize: 1024,
		EventID:    eventID,
		Payload:    payload,
		Now:        time.Now().UTC(),
	}
}

func outboxCount(t *testing.T, p *Pool, uploadID string) int {
	t.Helper()
	var n int
	err := p.inner.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1`, uploadID).Scan(&n)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestCreateGetOwnership(t *testing.T) {
	p := testPool(t)
	userID := uuid.NewString()
	u := seedUploading(t, p, userID)

	got, err := p.GetByIDForUser(context.Background(), userID, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ObjectKey != u.ObjectKey || got.Status != domain.StatusUploading {
		t.Fatalf("unexpected row: %+v", got)
	}
	if _, err := p.GetByIDForUser(context.Background(), uuid.NewString(), u.ID); err != repository.ErrNotFound {
		t.Fatalf("foreign user must get ErrNotFound, got %v", err)
	}
}

func TestMarkReadyAndCreateEvent(t *testing.T) {
	p := testPool(t)
	u := seedUploading(t, p, uuid.NewString())

	res, err := p.MarkReadyAndCreateEvent(context.Background(), completeParams(u))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.AlreadyCompleted || res.Upload.Status != domain.StatusReady {
		t.Fatalf("unexpected result: %+v", res.Upload)
	}
	if n := outboxCount(t, p, u.ID); n != 1 {
		t.Fatalf("want 1 outbox event, got %d", n)
	}

	res, err = p.MarkReadyAndCreateEvent(context.Background(), completeParams(u))
	if err != nil {
		t.Fatalf("repeat complete: %v", err)
	}
	if !res.AlreadyCompleted {
		t.Fatal("repeat /complete must report AlreadyCompleted")
	}
	if n := outboxCount(t, p, u.ID); n != 1 {
		t.Fatalf("repeat must not duplicate event, got %d", n)
	}

	if err := p.MarkSubmitted(context.Background(), u.UserID, u.ID, time.Now().UTC()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	got, _ := p.GetByIDForUser(context.Background(), u.UserID, u.ID)
	if got.Status != domain.StatusSubmitted {
		t.Fatalf("want SUBMITTED, got %s", got.Status)
	}
}

func TestConcurrentCompleteSingleEvent(t *testing.T) {
	p := testPool(t)
	u := seedUploading(t, p, uuid.NewString())

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = p.MarkReadyAndCreateEvent(context.Background(), completeParams(u))
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent complete: %v", err)
		}
	}
	if n := outboxCount(t, p, u.ID); n != 1 {
		t.Fatalf("5 concurrent /complete calls must yield 1 event, got %d", n)
	}
}
