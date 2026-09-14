package deletion

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"checkpoint/ingestion/internal/repository"
)

type fakePurge struct {
	jobs       []repository.AccountDeletionJob
	objects    []repository.UploadObject
	ingestion  int
	quarantine int
	downstream int
	completed  int
	retried    int
}

func (f *fakePurge) ClaimDeletions(context.Context, int, time.Time) ([]repository.AccountDeletionJob, error) {
	return f.jobs, nil
}

func (f *fakePurge) ListUploadObjects(context.Context, string) ([]repository.UploadObject, error) {
	return f.objects, nil
}

func (f *fakePurge) PurgeIngestion(context.Context, string) error {
	f.ingestion++
	return nil
}

func (f *fakePurge) QuarantineDevice(context.Context, string) error {
	f.quarantine++
	return nil
}

func (f *fakePurge) PurgeDownstream(context.Context, string) error {
	f.downstream++
	return nil
}

func (f *fakePurge) CompleteDeletion(context.Context, string, time.Time) error {
	f.completed++
	return nil
}

func (f *fakePurge) RetryDeletion(context.Context, string, time.Time, string, time.Time) error {
	f.retried++
	return nil
}

type fakeObjects struct {
	deleted []string
	failOn  string
}

func (f *fakeObjects) DeleteObject(_ context.Context, _, objectKey string) error {
	if f.failOn != "" && objectKey == f.failOn {
		return errors.New("delete failed")
	}
	f.deleted = append(f.deleted, objectKey)
	return nil
}

type fakeIdentities struct {
	deleted []string
}

func (f *fakeIdentities) DeleteIdentity(_ context.Context, identityID string) error {
	f.deleted = append(f.deleted, identityID)
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSweepPurgesUser(t *testing.T) {
	purge := &fakePurge{
		jobs: []repository.AccountDeletionJob{{UserID: "user-1", Attempt: 1}},
		objects: []repository.UploadObject{
			{Bucket: "audio", ObjectKey: "user-1/a"},
			{Bucket: "audio", ObjectKey: "user-1/b"},
		},
	}
	objects := &fakeObjects{}
	identities := &fakeIdentities{}

	NewWorker(purge, objects, identities, testLogger()).Sweep(context.Background())

	if len(objects.deleted) != 2 {
		t.Fatalf("deleted %d objects, want 2", len(objects.deleted))
	}
	if purge.ingestion != 1 || purge.quarantine != 1 || purge.downstream != 1 {
		t.Fatalf("purge steps: ingestion=%d quarantine=%d downstream=%d", purge.ingestion, purge.quarantine, purge.downstream)
	}
	if len(identities.deleted) != 1 {
		t.Fatalf("identity not deleted")
	}
	if purge.completed != 1 || purge.retried != 0 {
		t.Fatalf("completed=%d retried=%d, want 1/0", purge.completed, purge.retried)
	}
}

func TestSweepRetriesOnObjectFailure(t *testing.T) {
	purge := &fakePurge{
		jobs:    []repository.AccountDeletionJob{{UserID: "user-1", Attempt: 1}},
		objects: []repository.UploadObject{{Bucket: "audio", ObjectKey: "user-1/a"}},
	}
	objects := &fakeObjects{failOn: "user-1/a"}

	NewWorker(purge, objects, nil, testLogger()).Sweep(context.Background())

	if purge.retried != 1 || purge.completed != 0 {
		t.Fatalf("completed=%d retried=%d, want 0/1", purge.completed, purge.retried)
	}
	if purge.ingestion != 0 {
		t.Fatalf("purge must not run after object failure")
	}
}
