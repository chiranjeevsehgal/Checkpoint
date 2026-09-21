package service

import (
	"context"
	"errors"
	"testing"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

type fakeItemsRepo struct {
	status string
	window string
	limit  int
	offset int
}

func (f *fakeItemsRepo) ListTodos(_ context.Context, _, status string, limit, offset int) ([]repository.Todo, error) {
	f.status, f.limit, f.offset = status, limit, offset
	return nil, nil
}

func (f *fakeItemsRepo) SetTodoDone(_ context.Context, _ string, _ int64, _ bool) error { return nil }
func (f *fakeItemsRepo) DeleteTodo(_ context.Context, _ string, _ int64) error          { return nil }

func (f *fakeItemsRepo) ListReminders(_ context.Context, _, window string, limit, offset int) ([]repository.Reminder, error) {
	f.window, f.limit, f.offset = window, limit, offset
	return nil, nil
}

func (f *fakeItemsRepo) DeleteReminder(_ context.Context, _ string, _ int64) error { return nil }

func (f *fakeItemsRepo) ListInsights(_ context.Context, _ string, limit, offset int) ([]repository.Insight, error) {
	f.limit, f.offset = limit, offset
	return nil, nil
}

func (f *fakeItemsRepo) DeleteInsight(_ context.Context, _ string, _ int64) error { return nil }

func TestClampPage(t *testing.T) {
	for _, tc := range []struct {
		limit, offset, wantLimit, wantOffset int
	}{
		{0, 0, 50, 0},
		{-3, -5, 50, 0},
		{25, 10, 25, 10},
		{9999, 4, 200, 4},
	} {
		limit, offset := ClampPage(tc.limit, tc.offset)
		if limit != tc.wantLimit || offset != tc.wantOffset {
			t.Fatalf("ClampPage(%d,%d) = (%d,%d), want (%d,%d)",
				tc.limit, tc.offset, limit, offset, tc.wantLimit, tc.wantOffset)
		}
	}
}

func TestListTodosNormalizesStatus(t *testing.T) {
	repo := &fakeItemsRepo{}
	svc := NewItemsService(repo)

	if _, err := svc.ListTodos(context.Background(), "user", "", 0, 0); err != nil {
		t.Fatalf("empty status: %v", err)
	}
	if repo.status != "all" {
		t.Fatalf("empty status: got %q, want all", repo.status)
	}

	if _, err := svc.ListTodos(context.Background(), "user", "open", 0, 0); err != nil {
		t.Fatalf("open: %v", err)
	}
	if repo.status != "open" {
		t.Fatalf("open: got %q", repo.status)
	}

	if _, err := svc.ListTodos(context.Background(), "user", "bogus", 0, 0); !errors.Is(err, domain.ErrInvalidItemFilter) {
		t.Fatalf("bogus status: got %v, want ErrInvalidItemFilter", err)
	}
}

func TestListRemindersNormalizesWindow(t *testing.T) {
	repo := &fakeItemsRepo{}
	svc := NewItemsService(repo)

	if _, err := svc.ListReminders(context.Background(), "user", "", 0, 0); err != nil {
		t.Fatalf("empty window: %v", err)
	}
	if repo.window != "upcoming" {
		t.Fatalf("empty window: got %q, want upcoming", repo.window)
	}

	if _, err := svc.ListReminders(context.Background(), "user", "all", 0, 0); err != nil {
		t.Fatalf("all: %v", err)
	}
	if repo.window != "all" {
		t.Fatalf("all: got %q", repo.window)
	}

	if _, err := svc.ListReminders(context.Background(), "user", "bogus", 0, 0); !errors.Is(err, domain.ErrInvalidItemFilter) {
		t.Fatalf("bogus window: got %v, want ErrInvalidItemFilter", err)
	}
}
