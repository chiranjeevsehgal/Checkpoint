package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
)

type fakeItems struct {
	todos     []repository.Todo
	reminders []repository.Reminder
	insights  []repository.Insight
	status    string
	window    string
	limit     int
	offset    int
	doneID    int64
	doneValue bool
	deleted   int64
	err       error
}

func (f *fakeItems) ListTodos(_ context.Context, _, status string, limit, offset int) ([]repository.Todo, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.status, f.limit, f.offset = status, limit, offset
	return f.todos, nil
}

func (f *fakeItems) SetTodoDone(_ context.Context, _ string, id int64, done bool) error {
	if f.err != nil {
		return f.err
	}
	f.doneID, f.doneValue = id, done
	return nil
}

func (f *fakeItems) DeleteTodo(_ context.Context, _ string, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = id
	return nil
}

func (f *fakeItems) ListReminders(_ context.Context, _, window string, limit, offset int) ([]repository.Reminder, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.window, f.limit, f.offset = window, limit, offset
	return f.reminders, nil
}

func (f *fakeItems) DeleteReminder(_ context.Context, _ string, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = id
	return nil
}

func (f *fakeItems) ListInsights(_ context.Context, _ string, limit, offset int) ([]repository.Insight, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.limit, f.offset = limit, offset
	return f.insights, nil
}

func (f *fakeItems) DeleteInsight(_ context.Context, _ string, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = id
	return nil
}

func itemsRouter(items itemsService) http.Handler {
	return NewRouter(RouterDeps{
		Auth:    fakeAuthenticator{},
		Uploads: &fakeService{},
		Devices: &fakeDevices{owned: map[string]bool{}},
		Items:   items,
		Idem:    &fakeIdem{rows: map[string]repository.IdempotencyRecord{}},
		Metrics: metrics.NewRegistry(),
	})
}

func doItems(t *testing.T, r http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestListTodosDefaults(t *testing.T) {
	items := &fakeItems{todos: []repository.Todo{{ID: 1, Text: "buy milk"}, {ID: 2, Text: "call bank"}}}
	rec := doItems(t, itemsRouter(items), "GET", "/v1/me/todos", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if items.status != "" || items.limit != 50 || items.offset != 0 {
		t.Fatalf("forwarded: status=%q limit=%d offset=%d", items.status, items.limit, items.offset)
	}
	var resp itemList[todoView]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 || resp.NextOffset != nil {
		t.Fatalf("unexpected body: %+v", resp)
	}
}

func TestListTodosPagingAndClamp(t *testing.T) {
	items := &fakeItems{todos: []repository.Todo{{ID: 1, Text: "one"}}}
	rec := doItems(t, itemsRouter(items), "GET", "/v1/me/todos?status=open&limit=1&offset=1", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if items.status != "open" || items.limit != 1 || items.offset != 1 {
		t.Fatalf("filters: status=%q limit=%d offset=%d", items.status, items.limit, items.offset)
	}
	var resp itemList[todoView]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NextOffset == nil || *resp.NextOffset != 2 {
		t.Fatalf("next_offset: got %v, want 2", resp.NextOffset)
	}

	items = &fakeItems{todos: []repository.Todo{{ID: 1, Text: "one"}}}
	doItems(t, itemsRouter(items), "GET", "/v1/me/todos?limit=9999", "")
	if items.limit != 200 {
		t.Fatalf("limit clamp: got %d, want 200", items.limit)
	}
}

func TestListTodosTimestampViews(t *testing.T) {
	recorded := time.Date(2026, 9, 20, 6, 30, 0, 0, time.UTC)
	items := &fakeItems{todos: []repository.Todo{
		{ID: 1, Text: "synced", RecordedAt: &recorded, CreatedAt: recorded},
		{ID: 2, Text: "unsynced", CreatedAt: recorded},
	}}
	rec := doItems(t, itemsRouter(items), "GET", "/v1/me/todos", "")

	var resp itemList[todoView]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Items[0].RecordedAt == nil || *resp.Items[0].RecordedAt != "2026-09-20T06:30:00Z" {
		t.Fatalf("recorded_at: %+v", resp.Items[0])
	}
	if resp.Items[1].RecordedAt != nil {
		t.Fatalf("unsynced recorded_at must be null: %+v", resp.Items[1])
	}
}

func TestListItemsRejectsBadFilter(t *testing.T) {
	items := &fakeItems{err: domain.ErrInvalidItemFilter}
	rec := doItems(t, itemsRouter(items), "GET", "/v1/me/todos?status=bogus", "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != CodeInvalidRequest {
		t.Fatalf("code: got %q, want %q", env.Error.Code, CodeInvalidRequest)
	}
}

func TestListRemindersWindow(t *testing.T) {
	items := &fakeItems{reminders: []repository.Reminder{{ID: 1, Text: "stand up", Important: true}}}
	rec := doItems(t, itemsRouter(items), "GET", "/v1/me/reminders?window=past", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if items.window != "past" {
		t.Fatalf("window: got %q, want past", items.window)
	}
	var resp itemList[reminderView]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 || !resp.Items[0].Important {
		t.Fatalf("unexpected body: %+v", resp)
	}

	items = &fakeItems{}
	doItems(t, itemsRouter(items), "GET", "/v1/me/reminders", "")
	if items.window != "" {
		t.Fatalf("handler should forward an empty window, got %q", items.window)
	}
}

func TestListInsights(t *testing.T) {
	items := &fakeItems{insights: []repository.Insight{{ID: 5, Text: "you sleep late"}}}
	rec := doItems(t, itemsRouter(items), "GET", "/v1/me/insights", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp itemList[insightView]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Text != "you sleep late" {
		t.Fatalf("unexpected body: %+v", resp)
	}
}

func TestSetTodoDone(t *testing.T) {
	items := &fakeItems{}
	rec := doItems(t, itemsRouter(items), "PATCH", "/v1/me/todos/42", `{"is_done":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if items.doneID != 42 || !items.doneValue {
		t.Fatalf("toggle: id=%d done=%v", items.doneID, items.doneValue)
	}
}

func TestSetTodoDoneRejectsBadBody(t *testing.T) {
	for _, tc := range []struct{ name, target, body string }{
		{"missing is_done", "/v1/me/todos/1", `{}`},
		{"malformed json", "/v1/me/todos/1", `{bad`},
		{"bad id", "/v1/me/todos/abc", `{"is_done":true}`},
	} {
		rec := doItems(t, itemsRouter(&fakeItems{}), "PATCH", tc.target, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: got %d, want 400", tc.name, rec.Code)
		}
	}
}

func TestDeleteItems(t *testing.T) {
	for _, tc := range []struct{ name, target string }{
		{"todo", "/v1/me/todos/7"},
		{"reminder", "/v1/me/reminders/7"},
		{"insight", "/v1/me/insights/7"},
	} {
		items := &fakeItems{}
		rec := doItems(t, itemsRouter(items), "DELETE", tc.target, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		if items.deleted != 7 {
			t.Fatalf("%s: deleted id=%d, want 7", tc.name, items.deleted)
		}
	}
}

func TestItemsRequireAuth(t *testing.T) {
	r := itemsRouter(&fakeItems{})
	for _, tc := range []struct{ method, target, body string }{
		{"GET", "/v1/me/todos", ""},
		{"PATCH", "/v1/me/todos/1", `{"is_done":true}`},
		{"DELETE", "/v1/me/todos/1", ""},
		{"GET", "/v1/me/reminders", ""},
		{"DELETE", "/v1/me/reminders/1", ""},
		{"GET", "/v1/me/insights", ""},
		{"DELETE", "/v1/me/insights/1", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: got %d, want 401", tc.method, tc.target, rec.Code)
		}
	}
}
