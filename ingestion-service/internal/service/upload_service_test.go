package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/storage"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

type fakeUploads struct {
	rows     map[string]*domain.Upload
	claimed  map[string][]byte
	hashes   map[string]string
	uploadID map[string]string
}

func (f *fakeUploads) Create(_ context.Context, u *domain.Upload) error {
	f.rows[u.ID] = u
	return nil
}

// CreateUploadIdempotent emulates the key claim: the first caller wins,
// later callers replay the stored body with the original hash.
func (f *fakeUploads) CreateUploadIdempotent(_ context.Context, p repository.IdempotentCreateParams) (*repository.IdempotentCreateResult, error) {
	if f.claimed == nil {
		f.claimed = map[string][]byte{}
		f.hashes = map[string]string{}
		f.uploadID = map[string]string{}
	}
	k := p.UserID + "/" + p.Key
	if body, ok := f.claimed[k]; ok {
		return &repository.IdempotentCreateResult{Replay: true, Stored: &repository.IdempotencyRecord{
			Key: p.Key, UserID: p.UserID, RequestHash: f.hashes[k],
			ResponseStatus: 201, ResponseBody: body,
		}}, nil
	}
	f.claimed[k] = p.ResponseBody
	f.hashes[k] = p.RequestHash
	f.uploadID[k] = p.Upload.ID
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
	events      int
	committed   map[string]bool
	lastPayload []byte
}

func (f *fakeCompletion) MarkReadyAndCreateEvent(_ context.Context, p repository.CompleteParams) (*repository.CompleteResult, error) {
	f.events++
	f.committed[p.UploadID] = true
	f.lastPayload = append([]byte(nil), p.Payload...)
	return &repository.CompleteResult{Upload: &domain.Upload{ID: p.UploadID, Status: domain.StatusReady}}, nil
}

type fakeStorage struct {
	size        int64
	contentType string
	statCalls   int
	statErr     error
	presignN    int
}

func (f *fakeStorage) CreateUploadURL(_ context.Context, _, _, _ string, _ time.Duration) (string, error) {
	f.presignN++
	if f.presignN == 1 {
		return "https://minio.test/put", nil
	}
	return "https://minio.test/put-fresh", nil
}

func (f *fakeStorage) StatObject(_ context.Context, _, _ string) (storage.ObjectInfo, error) {
	f.statCalls++
	if f.statErr != nil {
		return storage.ObjectInfo{}, f.statErr
	}
	ct := f.contentType
	if ct == "" {
		ct = "audio/ogg"
	}
	return storage.ObjectInfo{Size: f.size, ContentType: ct}, nil
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

func TestCompleteEmitsTranscriptionEvent(t *testing.T) {
	svc, _, completion, _ := newTestService(100)
	res, _ := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100,
	})
	checksum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID,
		CompleteCommand{Checksum: checksum}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	var payload domain.AudioReadyPayload
	if err := jsonUnmarshal(completion.lastPayload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.EventType != domain.EventTranscriptionRequested {
		t.Fatalf("event type: got %q", payload.EventType)
	}
	if payload.Data.UserID != "user-1" || payload.Data.AudioID != res.Upload.ID {
		t.Fatalf("identity: %+v", payload.Data)
	}
	if payload.Data.ChecksumSHA256 != checksum || payload.Data.Bucket == "" || payload.Data.ObjectKey == "" {
		t.Fatalf("by-reference/checksum: %+v", payload.Data)
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

func TestCreateNormalizes(t *testing.T) {
	svc, uploads, _, _ := newTestService(100)
	res, err := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "  Meeting.OGG  ", ContentType: "  Audio/OGG  ", SizeBytes: 100,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got := uploads.rows[res.Upload.ID]
	if got.OriginalFilename != "Meeting.OGG" || got.ContentType != "audio/ogg" {
		t.Fatalf("not normalized: %+v", got)
	}
}

func TestCompleteRejectsContentTypeMismatch(t *testing.T) {
	svc, _, _, st := newTestService(100)
	st.contentType = "audio/mpeg"
	res, _ := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100,
	})
	if _, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID, CompleteCommand{}); !errors.Is(err, domain.ErrUnsupportedMediaType) {
		t.Fatalf("want ErrUnsupportedMediaType, got %v", err)
	}
}

func TestCompleteActualTooLarge(t *testing.T) {
	svc, _, _, _ := newTestService(domain.MaxUploadBytes + 1)
	res, _ := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100,
	})
	if _, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID, CompleteCommand{}); !errors.Is(err, domain.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestCompleteNegativeSizeAndBadChecksum(t *testing.T) {
	svc, _, _, _ := newTestService(100)
	res, _ := svc.CreateUpload(context.Background(), "user-1", CreateCommand{
		Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100,
	})
	if _, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID, CompleteCommand{SizeBytes: -5}); !errors.Is(err, domain.ErrInvalidSize) {
		t.Fatalf("negative size must be 400, got %v", err)
	}
	if _, err := svc.CompleteUpload(context.Background(), "user-1", res.Upload.ID, CompleteCommand{Checksum: "xyz"}); !errors.Is(err, domain.ErrInvalidChecksum) {
		t.Fatalf("bad checksum must fail, got %v", err)
	}
}

func TestIdempotentReplayRefreshesURL(t *testing.T) {
	svc, _, _, _ := newTestService(100)
	cmd := CreateCommand{Filename: "m.ogg", ContentType: "audio/ogg", SizeBytes: 100}
	encode := func(res *CreateResult) []byte {
		return []byte(`{"upload_id":"` + res.Upload.ID + `"}`)
	}
	fresh, err := svc.CreateUploadIdempotent(context.Background(), "user-1", cmd, "key-fresh", "hash-1", encode)
	if err != nil || fresh.Replay {
		t.Fatalf("fresh: %v %+v", err, fresh)
	}
	firstURL := fresh.Result.UploadURL
	replay, err := svc.CreateUploadIdempotent(context.Background(), "user-1", cmd, "key-fresh", "hash-1", encode)
	if err != nil || !replay.Replay {
		t.Fatalf("replay: %v %+v", err, replay)
	}
	if replay.Result == nil || len(replay.Body) == 0 {
		t.Fatalf("replay must carry fresh URL, got %+v", replay)
	}
	if replay.Result.UploadURL == firstURL {
		t.Fatalf("fresh URL must differ: %q", firstURL)
	}
	if replay.Result.Upload.ID != fresh.Result.Upload.ID {
		t.Fatalf("upload_id must be stable")
	}
}
