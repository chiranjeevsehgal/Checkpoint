package http

import (
	"context"
	"encoding/json"
	"net/http"

	"checkpoint/ingestion/internal/service"
)

// notificationService is the subset of service.NotificationService used by HTTP.
type notificationService interface {
	Get(ctx context.Context, userID string) (*service.NotificationChannel, error)
	Enable(ctx context.Context, userID string) (*service.NotificationChannel, error)
	Disable(ctx context.Context, userID string) error
	SetAdvance(ctx context.Context, userID string, minutes int) (*service.NotificationChannel, error)
}

// NotificationHandler serves the per-user ntfy channel API.
type NotificationHandler struct {
	notifications notificationService
	ntfyURL       string
}

// NewNotificationHandler wires the notification endpoints.
func NewNotificationHandler(notifications notificationService, ntfyURL string) *NotificationHandler {
	return &NotificationHandler{notifications: notifications, ntfyURL: ntfyURL}
}

type notificationResponse struct {
	Enabled        bool   `json:"enabled"`
	NtfyURL        string `json:"ntfy_url"`
	Topic          string `json:"topic,omitempty"`
	Token          string `json:"token,omitempty"`
	AdvanceMinutes int    `json:"advance_minutes,omitempty"`
}

type updateNotificationRequest struct {
	AdvanceMinutes *int `json:"advance_minutes"`
}

// Get handles GET /v1/me/notifications.
func (h *NotificationHandler) Get(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	channel, err := h.notifications.Get(r.Context(), principal.UserID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.response(channel))
}

// Enable handles POST /v1/me/notifications.
func (h *NotificationHandler) Enable(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	channel, err := h.notifications.Enable(r.Context(), principal.UserID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.response(channel))
}

// Disable handles DELETE /v1/me/notifications.
func (h *NotificationHandler) Disable(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	if err := h.notifications.Disable(r.Context(), principal.UserID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, notificationResponse{Enabled: false, NtfyURL: h.ntfyURL})
}

// Put handles PUT /v1/me/notifications, updating the advance lead time.
func (h *NotificationHandler) Put(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	raw, ok := readRawBody(w, r, 1<<20)
	if !ok {
		return
	}
	var req updateNotificationRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}
	if req.AdvanceMinutes == nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "advance_minutes is required.")
		return
	}
	channel, err := h.notifications.SetAdvance(r.Context(), principal.UserID, *req.AdvanceMinutes)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.response(channel))
}

func (h *NotificationHandler) response(channel *service.NotificationChannel) notificationResponse {
	return notificationResponse{
		Enabled:        channel.Enabled,
		NtfyURL:        h.ntfyURL,
		Topic:          channel.Topic,
		Token:          channel.Token,
		AdvanceMinutes: channel.AdvanceMinutes,
	}
}
