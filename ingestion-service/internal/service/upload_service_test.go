package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/storage"
)

type fakeUploads struct {
	rows    map[string]*domain.Upload
	claimed map[string][]byte
}

func (f *fakeUploads) Create(_ context.Context, u *domain.Upload) error {
	f.rows[u.ID] = u
	return nil
}

// CreateUploadIdempotent emulates the key claim: the first caller wins,
// later callers replay the stored body.
func (f *fakeUploads) CreateUploadIdempotent(_ context.Context, p repository.IdempotentCreateParams) (*repository.IdempotentCreateResult, error) {
	if f.claimed == nil {
		f.claimed = map[string][]byte{}
	}
	if body, ok := f.claimed[p.UserID+"/"+p.Key]; ok {
		return &repository.IdempotentCreateResult{Replay: true, Stored: &repository.IdempotencyRecord{
			Key: p.Key, UserID: p.UserID, RequestHash: p.RequestHash,
			ResponseStatus: p.ResponseStatus, ResponseBody: body,
		}}, nil
	}
	f.claimed[p.UserID+"/"+p.Key] = p.ResponseBody
	f.rows[p.Upload.ID] = p.Upload
	return &repository.IdempotentCreateResult{}, nil
}

func (f *fakeUploads) GetByIDForUser(_ context.Context, userID, uploadID string) (*domain.Upload, error) {
	u, ok := f.rows[uploadID]
	if !ok || u.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return u, nil
}

type fakeCompletion struct {
	events    int
	committed map[string]bool
}

func (f *fakeCompletion) MarkReadyAndCreateEvent(_ context.Context, p repository.CompleteParams) (*repository.CompleteResult, error) {
	f.events++
	f.committed[p.UploadID] = true
	return &repository.CompleteResult{Upload: &domain.Upload{ID: p.UploadID, Status: domain.StatusReady}}, nil
}

func (f *fakeCompletion) MarkSubmitted(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

type fakeStorage struct {
	size      int64
	statCalls int
	statErr   error
}

func (f *fakeStorage) CreateUploadURL(_ context.Context, _, _, _ string, _ time.Duration) (string, error) {
	return "https://minio.test/put", nil
}

func (f *fakeStorage) StatObject(_ context.Context, _, _ string) (storage.ObjectInfo, error) {
	f.statCalls++
	if f.statErr != nil {
		return storage.ObjectInfo{}, f.statErr
	}
	return storage.ObjectInfo{Size: f.size, ContentType: "audio/ogg"}, nil
}

func (f *fakeStorage) DeleteObject(_ context.Context, _, _ string) error { return nil }

func newTestService(size int64) (*UploadService, *fakeUploads, *fakeCompletion, *fakeStorage) {
	uploads := &fakeUploads{rows: map[string]*domain.Upload{}}
	completion := &fakeCompletion{committed: map[string]bool{}}
	st := &fakeStorage{size: size}
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	svc := NewUploadService(uploads, completion, st, "audio", func() time.Time { return now })
	return svc, uploads, completion, st
}

func TestCreateUpload(t *testing.T) {
	svc, _, _, _ := newTestService(100)
	res, err := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "meeting.ogg", ContentType: "audio/ogg", SizeBytes: 100,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Upload.Status != domain.StatusUploading || res.UploadURL == "" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if _, err := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "a.bin", ContentType: "application/octet-stream", SizeBytes: 100,
	}); !errors.Is(err, domain.ErrUnsupportedMediaType) {
		t.Fatalf("want ErrUnsupportedMediaType, got %v", err)
	}
}

func TestCompleteUpload(t *testing.T) {
	svc, uploads, completion, st := newTestService(100)
	res, _ := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100,
	})
	u, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID, CompleteCommand{})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if u.Status != domain.StatusReady || completion.events != 1 {
		t.Fatalf("unexpected: %+v events=%d", u, completion.events)
	}

	// Idempotent repeat must not touch MinIO or emit another event.
	uploads.rows[res.Upload.ID].Status = domain.StatusReady
	calls := st.statCalls
	if _, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID, CompleteCommand{}); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if st.statCalls != calls || completion.events != 1 {
		t.Fatal("repeat /complete must skip StatObject and event insert")
	}
}

func TestCompleteSizeMismatch(t *testing.T) {
	svc, _, _, _ := newTestService(100)
	res, _ := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100,
	})
	if _, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID,
		CompleteCommand{SizeBytes: 999}); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("want ErrSizeMismatch, got %v", err)
	}
}

func TestCompleteMissingObject(t *testing.T) {
	svc, _, _, st := newTestService(0)
	st.statErr = storage.ErrObjectNotFound
	res, _ := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100,
	})
	if _, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID, CompleteCommand{}); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("want ErrObjectNotFound, got %v", err)
	}
}

func TestCreateUploadIdempotentFreshThenReplay(t *testing.T) {
	svc, uploads, _, _ := newTestService(100)
	cmd := CreateCommand{Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100}
	encode := func(res *CreateResult) []byte { return []byte("body-for-" + res.Upload.ID) }

	fresh, err := svc.CreateUploadIdempotent(context.Background(), "user-1", cmd, "key-1", "hash-1", encode)
	if err != nil {
		t.Fatalf("fresh: %v", err)
	}
	if fresh.Replay || fresh.Result == nil || string(fresh.Body) != "body-for-"+fresh.Result.Upload.ID {
		t.Fatalf("unexpected fresh outcome: %+v", fresh)
	}

	replay, err := svc.CreateUploadIdempotent(context.Background(), "user-1", cmd, "key-1", "hash-1", encode)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.Replay || replay.Stored == nil || string(replay.Stored.ResponseBody) != string(fresh.Body) {
		t.Fatalf("replay must return the stored body, got %+v", replay)
	}
	if len(uploads.rows) != 1 {
		t.Fatalf("want 1 upload row, got %d", len(uploads.rows))
	}
}

func TestCreateUploadIdempotentValidationFirst(t *testing.T) {
	svc, _, _, _ := newTestService(100)
	encode := func(res *CreateResult) []byte { return []byte("x") }
	if _, err := svc.CreateUploadIdempotent(context.Background(), "user-1",
		CreateCommand{Filename: "a.bin", ContentType: "application/octet-stream", SizeBytes: 100},
		"key-1", "hash-1", encode); !errors.Is(err, domain.ErrUnsupportedMediaType) {
		t.Fatalf("invalid requests must fail before claiming a key, got %v", err)
	}
}
