package http

import (
	"bytes"
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
	ListTodos(ctx context.Context, userID, status string, limit, offset int) ([]repository.Todo, int, error)
	UpdateTodo(ctx context.Context, userID string, id int64, update repository.TodoUpdate) (*repository.Todo, error)
	DeleteTodo(ctx context.Context, userID string, id int64) error
	ListReminders(ctx context.Context, userID, window string, limit, offset int) ([]repository.Reminder, int, error)
	UpdateReminder(ctx context.Context, userID string, id int64, update repository.ReminderUpdate) (*repository.Reminder, error)
	DeleteReminder(ctx context.Context, userID string, id int64) error
	ListInsights(ctx context.Context, userID string, limit, offset int) ([]repository.Insight, int, error)
	UpdateInsight(ctx context.Context, userID string, id int64, text string) (*repository.Insight, error)
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

// itemList is the paged shape shared by every list endpoint. NextOffset is nil
// once the last page has been returned; Total is the count matching the filter.
type itemList[T any] struct {
	Items      []T  `json:"items"`
	NextOffset *int `json:"next_offset"`
	Total      int  `json:"total"`
}

type updateTodoRequest struct {
	Text   *string `json:"text"`
	IsDone *bool   `json:"is_done"`
}

type updateReminderRequest struct {
	Text      *string         `json:"text"`
	RemindAt  json.RawMessage `json:"remind_at"`
	Important *bool           `json:"important"`
}

type updateInsightRequest struct {
	Text *string `json:"text"`
}

// ListTodos handles GET /v1/me/todos.
func (h *ItemsHandler) ListTodos(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	limit, offset := service.ClampPage(intParam(r, "limit"), intParam(r, "offset"))
	todos, total, err := h.items.ListTodos(r.Context(), principal.UserID, r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	views := make([]todoView, 0, len(todos))
	for i := range todos {
		views = append(views, toTodoView(&todos[i]))
	}
	writeJSON(w, http.StatusOK, itemList[todoView]{
		Items: views, NextOffset: nextOffset(offset, len(views), limit), Total: total,
	})
}

// UpdateTodo handles PATCH /v1/me/todos/{id}. Only the fields present in the
// body are written, so text and completion save independently.
func (h *ItemsHandler) UpdateTodo(w http.ResponseWriter, r *http.Request) {
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
	var req updateTodoRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}
	if req.Text == nil && req.IsDone == nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Provide text or is_done.")
		return
	}
	todo, err := h.items.UpdateTodo(r.Context(), principal.UserID, id, repository.TodoUpdate{
		Text: req.Text, IsDone: req.IsDone,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTodoView(todo))
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
	reminders, total, err := h.items.ListReminders(r.Context(), principal.UserID, r.URL.Query().Get("window"), limit, offset)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	views := make([]reminderView, 0, len(reminders))
	for i := range reminders {
		views = append(views, toReminderView(&reminders[i]))
	}
	writeJSON(w, http.StatusOK, itemList[reminderView]{
		Items: views, NextOffset: nextOffset(offset, len(views), limit), Total: total,
	})
}

// UpdateReminder handles PATCH /v1/me/reminders/{id}. A null remind_at clears
// the due time.
func (h *ItemsHandler) UpdateReminder(w http.ResponseWriter, r *http.Request) {
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
	var req updateReminderRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}
	if req.Text == nil && req.RemindAt == nil && req.Important == nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Provide text, remind_at or important.")
		return
	}
	update := repository.ReminderUpdate{Text: req.Text, Important: req.Important}
	if req.RemindAt != nil {
		update.RemindAtSet = true
		if !isJSONNull(req.RemindAt) {
			var rawTime string
			if err := json.Unmarshal(req.RemindAt, &rawTime); err != nil {
				writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "remind_at must be an RFC3339 string or null.")
				return
			}
			remindAt, err := time.Parse(time.RFC3339, rawTime)
			if err != nil {
				writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "remind_at must be an RFC3339 string or null.")
				return
			}
			update.RemindAt = &remindAt
		}
	}
	reminder, err := h.items.UpdateReminder(r.Context(), principal.UserID, id, update)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toReminderView(reminder))
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
	insights, total, err := h.items.ListInsights(r.Context(), principal.UserID, limit, offset)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	views := make([]insightView, 0, len(insights))
	for i := range insights {
		views = append(views, toInsightView(&insights[i]))
	}
	writeJSON(w, http.StatusOK, itemList[insightView]{
		Items: views, NextOffset: nextOffset(offset, len(views), limit), Total: total,
	})
}

// UpdateInsight handles PATCH /v1/me/insights/{id}.
func (h *ItemsHandler) UpdateInsight(w http.ResponseWriter, r *http.Request) {
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
	var req updateInsightRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}
	if req.Text == nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "text is required.")
		return
	}
	insight, err := h.items.UpdateInsight(r.Context(), principal.UserID, id, *req.Text)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toInsightView(insight))
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

// isJSONNull reports whether a raw JSON field was an explicit null.
func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
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
