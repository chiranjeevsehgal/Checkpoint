package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"checkpoint/ingestion/internal/repository"
)

type fakeStore struct {
	stale     []repository.ExpiredUpload
	olderThan time.Time
	nowArg    time.Time
}

func (f *fakeStore) ExpireStaleUploads(_ context.Context, olderThan, now time.Time) ([]repository.ExpiredUpload, error) {
	f.olderThan = olderThan
	f.nowArg = now
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

func TestSweepCutoff(t *testing.T) {
	store := &fakeStore{}
	fixed := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	c := NewCleaner(store, &fakeDeleter{}, 24*time.Hour, time.Minute, nil)
	c.now = func() time.Time { return fixed }
	if _, _, err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.olderThan.Equal(fixed.Add(-24 * time.Hour)) || !store.nowArg.Equal(fixed) {
		t.Fatalf("cutoff wrong: olderThan=%v now=%v", store.olderThan, store.nowArg)
	}
}
