package http

import (
	"context"
	"encoding/json"
	"net/http"

	"checkpoint/ingestion/internal/service"
)

// settingsService is the subset of service.SettingsService used by HTTP.
type settingsService interface {
	Get(ctx context.Context, userID string) (*service.Settings, error)
	SetLanguages(ctx context.Context, userID string, codes []string) ([]string, error)
}

// SettingsHandler serves the per-user preferences API.
type SettingsHandler struct {
	settings settingsService
}

// NewSettingsHandler wires the settings endpoints.
func NewSettingsHandler(settings settingsService) *SettingsHandler {
	return &SettingsHandler{settings: settings}
}

type languageOption struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type settingsResponse struct {
	Languages []string         `json:"languages"`
	Available []languageOption `json:"available"`
}

type updateSettingsRequest struct {
	Languages []string `json:"languages"`
}

type updateSettingsResponse struct {
	Languages []string `json:"languages"`
}

// Get handles GET /v1/me/settings, returning the selection and the catalog.
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	settings, err := h.settings.Get(r.Context(), principal.UserID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	available := make([]languageOption, 0, len(settings.Available))
	for _, language := range settings.Available {
		available = append(available, languageOption{Code: language.Code, Name: language.Name})
	}
	writeJSON(w, http.StatusOK, settingsResponse{Languages: settings.Languages, Available: available})
}

// Put handles PUT /v1/me/settings. An empty list clears filtering.
func (h *SettingsHandler) Put(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	raw, ok := readRawBody(w, r, 1<<20)
	if !ok {
		return
	}
	var req updateSettingsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}
	languages, err := h.settings.SetLanguages(r.Context(), principal.UserID, req.Languages)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updateSettingsResponse{Languages: languages})
}
