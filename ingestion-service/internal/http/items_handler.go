package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/service"
)

// itemsService is the subset of service.ItemsService used by HTTP.
type itemsService interface {
	ListTodos(ctx context.Context, userID, status string, limit, offset int) ([]repository.Todo, error)
	SetTodoDone(ctx context.Context, userID string, id int64, done bool) error
	DeleteTodo(ctx context.Context, userID string, id int64) error
	ListReminders(ctx context.Context, userID, window string, limit, offset int) ([]repository.Reminder, error)
	DeleteReminder(ctx context.Context, userID string, id int64) error
	ListInsights(ctx context.Context, userID string, limit, offset int) ([]repository.Insight, error)
	DeleteInsight(ctx context.Context, userID string, id int64) error
}

// ItemsHandler serves the extracted to-do, reminder and insight lists.
type ItemsHandler struct {
	items itemsService
}

// NewItemsHandler wires the items endpoints.
func NewItemsHandler(items itemsService) *ItemsHandler {
	return &ItemsHandler{items: items}
}

type todoView struct {
	ID         int64   `json:"id"`
	Text       string  `json:"text"`
	IsDone     bool    `json:"is_done"`
	AudioID    string  `json:"audio_id"`
	RecordedAt *string `json:"recorded_at"`
	CreatedAt  string  `json:"created_at"`
}

type reminderView struct {
	ID        int64   `json:"id"`
	Text      string  `json:"text"`
	RemindAt  *string `json:"remind_at"`
	Important bool    `json:"important"`
	AudioID   string  `json:"audio_id"`
	CreatedAt string  `json:"created_at"`
}

type insightView struct {
	ID        int64  `json:"id"`
	Text      string `json:"text"`
	AudioID   string `json:"audio_id"`
	CreatedAt string `json:"created_at"`
}

// itemList is the paged shape shared by every list endpoint. NextOffset is
// nil once the last page has been returned.
type itemList[T any] struct {
	Items      []T  `json:"items"`
	NextOffset *int `json:"next_offset"`
}

type setTodoDoneRequest struct {
	IsDone *bool `json:"is_done"`
}

// ListTodos handles GET /v1/me/todos.
func (h *ItemsHandler) ListTodos(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	limit, offset := service.ClampPage(intParam(r, "limit"), intParam(r, "offset"))
	todos, err := h.items.ListTodos(r.Context(), principal.UserID, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	views := make([]todoView, 0, len(todos))
	for i := range todos {
		views = append(views, toTodoView(&todos[i]))
	}
	writeJSON(w, http.StatusOK, itemList[todoView]{Items: views, NextOffset: nextOffset(offset, len(views), limit)})
}

// SetTodoDone handles PATCH /v1/me/todos/{id}.
func (h *ItemsHandler) SetTodoDone(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	raw, ok := readRawBody(w, r, 1<<20)
	if !ok {
		return
	}
	var req setTodoDoneRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}
	if req.IsDone == nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "is_done is required.")
		return
	}
	if err := h.items.SetTodoDone(r.Context(), principal.UserID, id, *req.IsDone); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"is_done": *req.IsDone})
}

// DeleteTodo handles DELETE /v1/me/todos/{id}.
func (h *ItemsHandler) DeleteTodo(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	if err := h.items.DeleteTodo(r.Context(), principal.UserID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ListReminders handles GET /v1/me/reminders.
func (h *ItemsHandler) ListReminders(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	limit, offset := service.ClampPage(intParam(r, "limit"), intParam(r, "offset"))
	reminders, err := h.items.ListReminders(r.Context(), principal.UserID, r.URL.Query().Get("window"), limit, offset)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	views := make([]reminderView, 0, len(reminders))
	for i := range reminders {
		views = append(views, toReminderView(&reminders[i]))
	}
	writeJSON(w, http.StatusOK, itemList[reminderView]{Items: views, NextOffset: nextOffset(offset, len(views), limit)})
}

// DeleteReminder handles DELETE /v1/me/reminders/{id}.
func (h *ItemsHandler) DeleteReminder(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	if err := h.items.DeleteReminder(r.Context(), principal.UserID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ListInsights handles GET /v1/me/insights.
func (h *ItemsHandler) ListInsights(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	limit, offset := service.ClampPage(intParam(r, "limit"), intParam(r, "offset"))
	insights, err := h.items.ListInsights(r.Context(), principal.UserID, limit, offset)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	views := make([]insightView, 0, len(insights))
	for i := range insights {
		views = append(views, toInsightView(&insights[i]))
	}
	writeJSON(w, http.StatusOK, itemList[insightView]{Items: views, NextOffset: nextOffset(offset, len(views), limit)})
}

// DeleteInsight handles DELETE /v1/me/insights/{id}.
func (h *ItemsHandler) DeleteInsight(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	if err := h.items.DeleteInsight(r.Context(), principal.UserID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// itemID parses the {id} path value shared by the mutation endpoints.
func itemID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Invalid item id.")
		return 0, false
	}
	return id, true
}

// intParam reads an optional integer query parameter, falling back to zero so
// the service applies its default.
func intParam(r *http.Request, name string) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

// nextOffset returns the offset for the following page, or nil at the end.
func nextOffset(offset, returned, limit int) *int {
	if returned < limit {
		return nil
	}
	next := offset + returned
	return &next
}

func toTodoView(todo *repository.Todo) todoView {
	return todoView{
		ID:         todo.ID,
		Text:       todo.Text,
		IsDone:     todo.IsDone,
		AudioID:    todo.AudioID,
		RecordedAt: utcStringPtr(todo.RecordedAt),
		CreatedAt:  utcString(todo.CreatedAt),
	}
}

func toReminderView(reminder *repository.Reminder) reminderView {
	return reminderView{
		ID:        reminder.ID,
		Text:      reminder.Text,
		RemindAt:  utcStringPtr(reminder.RemindAt),
		Important: reminder.Important,
		AudioID:   reminder.AudioID,
		CreatedAt: utcString(reminder.CreatedAt),
	}
}

func toInsightView(insight *repository.Insight) insightView {
	return insightView{
		ID:        insight.ID,
		Text:      insight.Text,
		AudioID:   insight.AudioID,
		CreatedAt: utcString(insight.CreatedAt),
	}
}

func utcString(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func utcStringPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	value := utcString(*t)
	return &value
}
