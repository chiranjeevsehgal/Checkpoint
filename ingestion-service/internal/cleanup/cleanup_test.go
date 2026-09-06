package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/repository"
)

type fakeStore struct {
	stale []repository.ExpiredUpload
}

func (f *fakeStore) ExpireStaleUploads(_ context.Context, _, _ time.Time) ([]repository.ExpiredUpload, error) {
	return f.stale, nil
}

type fakeDeleter struct {
	deleted []string
	failKey string
}

func (f *fakeDeleter) DeleteObject(_ context.Context, _, key string) error {
	if key == f.failKey {
		return errors.New("minio down")
	}
	f.deleted = append(f.deleted, key)
	return nil
}

func TestSweep(t *testing.T) {
	c := NewCleaner(
		&fakeStore{stale: []repository.ExpiredUpload{
			{Bucket: "audio", ObjectKey: "k1"},
			{Bucket: "audio", ObjectKey: "k2"},
		}},
		&fakeDeleter{}, time.Hour, time.Minute, nil,
	)
	expired, deleted, err := c.Sweep(context.Background())
	if err != nil || expired != 2 || deleted != 2 {
		t.Fatalf("expired=%d deleted=%d err=%v", expired, deleted, err)
	}
}

func TestSweepDeleteFailureIsNonFatal(t *testing.T) {
	d := &fakeDeleter{failKey: "k1"}
	c := NewCleaner(
		&fakeStore{stale: []repository.ExpiredUpload{
			{Bucket: "audio", ObjectKey: "k1"},
			{Bucket: "audio", ObjectKey: "k2"},
		}},
		d, time.Hour, time.Minute, nil,
	)
	expired, deleted, err := c.Sweep(context.Background())
	if err != nil || expired != 2 || deleted != 1 {
		t.Fatalf("expired=%d deleted=%d err=%v", expired, deleted, err)
	}
}
