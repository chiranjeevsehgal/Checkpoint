package identitycleanup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"checkpoint/ingestion/internal/auth"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeLister struct {
	pages [][]auth.Identity
	calls int
	err   error
}

func (f *fakeLister) ListIdentities(_ context.Context, _ int, _ string) ([]auth.Identity, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	if f.calls >= len(f.pages) {
		return nil, "", nil
	}
	page := f.pages[f.calls]
	f.calls++
	if f.calls >= len(f.pages) {
		return page, "", nil
	}
	return page, fmt.Sprintf("token-%d", f.calls), nil
}

type fakeDeleter struct {
	deleted []string
	failFor map[string]bool
}

func (f *fakeDeleter) DeleteIdentity(_ context.Context, id string) error {
	if f.failFor[id] {
		return errors.New("delete failed")
	}
	f.deleted = append(f.deleted, id)
	return nil
}

func identity(id string, createdAt time.Time, verified bool) auth.Identity {
	status := "sent"
	if verified {
		status = "completed"
	}
	return auth.Identity{
		ID:                  id,
		CreatedAt:           createdAt,
		VerifiableAddresses: []auth.VerifiableAddress{{Via: "email", Verified: verified, Status: status}},
	}
}

func TestSweepDeletesOnlyStaleUnverified(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	lister := &fakeLister{pages: [][]auth.Identity{{
		identity("verified-stale", now.Add(-5*time.Hour), true),
		identity("unverified-stale", now.Add(-2*time.Hour), false),
		identity("unverified-fresh", now.Add(-30*time.Minute), false),
	}}}
	deleter := &fakeDeleter{}
	worker := NewWorker(lister, deleter, time.Hour, time.Minute, newTestLogger())
	worker.now = func() time.Time { return now }

	deleted, err := worker.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if len(deleter.deleted) != 1 || deleter.deleted[0] != "unverified-stale" {
		t.Fatalf("deleted IDs = %v, want [unverified-stale]", deleter.deleted)
	}
}

func TestSweepWalksAllPages(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	lister := &fakeLister{pages: [][]auth.Identity{
		{identity("page-one", now.Add(-2*time.Hour), false)},
		{identity("page-two", now.Add(-2*time.Hour), false)},
	}}
	deleter := &fakeDeleter{}
	worker := NewWorker(lister, deleter, time.Hour, time.Minute, newTestLogger())
	worker.now = func() time.Time { return now }

	deleted, err := worker.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 2 || lister.calls != 2 {
		t.Fatalf("deleted = %d, calls = %d, want 2, 2", deleted, lister.calls)
	}
}

func TestSweepContinuesAfterDeleteError(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	lister := &fakeLister{pages: [][]auth.Identity{{
		identity("fails", now.Add(-2*time.Hour), false),
		identity("succeeds", now.Add(-2*time.Hour), false),
	}}}
	deleter := &fakeDeleter{failFor: map[string]bool{"fails": true}}
	worker := NewWorker(lister, deleter, time.Hour, time.Minute, newTestLogger())
	worker.now = func() time.Time { return now }

	deleted, err := worker.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 || len(deleter.deleted) != 1 || deleter.deleted[0] != "succeeds" {
		t.Fatalf("deleted = %d, IDs = %v", deleted, deleter.deleted)
	}
}

func TestSweepPropagatesListError(t *testing.T) {
	worker := NewWorker(&fakeLister{err: errors.New("boom")}, &fakeDeleter{}, time.Hour, time.Minute, newTestLogger())
	if _, err := worker.Sweep(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestSweepCapsDeletions(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	page := make([]auth.Identity, maxDeletionsPerSweep+50)
	for i := range page {
		page[i] = identity(fmt.Sprintf("id-%d", i), now.Add(-2*time.Hour), false)
	}
	deleter := &fakeDeleter{}
	worker := NewWorker(&fakeLister{pages: [][]auth.Identity{page}}, deleter, time.Hour, time.Minute, newTestLogger())
	worker.now = func() time.Time { return now }

	deleted, err := worker.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != maxDeletionsPerSweep {
		t.Fatalf("deleted = %d, want %d", deleted, maxDeletionsPerSweep)
	}
}

func TestRunSweepsImmediately(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	lister := &fakeLister{pages: [][]auth.Identity{{identity("stale", now.Add(-2*time.Hour), false)}}}
	deleter := &fakeDeleter{}
	worker := NewWorker(lister, deleter, time.Hour, time.Minute, newTestLogger())
	worker.now = func() time.Time { return now }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.Run(ctx)

	if len(deleter.deleted) != 1 || deleter.deleted[0] != "stale" {
		t.Fatalf("deleted IDs = %v, want [stale]", deleter.deleted)
	}
}
