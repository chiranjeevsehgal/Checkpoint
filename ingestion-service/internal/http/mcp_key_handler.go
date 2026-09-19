package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"checkpoint/ingestion/internal/service"
)

// mcpKeyService is the subset of service.McpKeyService used by HTTP.
type mcpKeyService interface {
	Create(ctx context.Context, userID, name string) (*service.CreatedMcpKey, error)
	List(ctx context.Context, userID string) ([]service.McpKey, error)
	Revoke(ctx context.Context, userID string, id int64) error
}

// McpKeyHandler serves the per-account MCP access-key API.
type McpKeyHandler struct {
	keys mcpKeyService
}

// NewMcpKeyHandler wires the MCP key endpoints.
func NewMcpKeyHandler(keys mcpKeyService) *McpKeyHandler {
	return &McpKeyHandler{keys: keys}
}

type mcpKeyView struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
}

type mcpKeyListResponse struct {
	Keys []mcpKeyView `json:"keys"`
}

type createMcpKeyRequest struct {
	Name string `json:"name"`
}

type createMcpKeyResponse struct {
	mcpKeyView
	Key string `json:"key"`
}

// List handles GET /v1/me/mcp-keys.
func (h *McpKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	keys, err := h.keys.List(r.Context(), principal.UserID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	views := make([]mcpKeyView, 0, len(keys))
	for _, key := range keys {
		views = append(views, toMcpKeyView(key))
	}
	writeJSON(w, http.StatusOK, mcpKeyListResponse{Keys: views})
}

// Create handles POST /v1/me/mcp-keys. The secret is returned only here.
func (h *McpKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	raw, ok := readRawBody(w, r, 1<<20)
	if !ok {
		return
	}
	var req createMcpKeyRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}
	created, err := h.keys.Create(r.Context(), principal.UserID, req.Name)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, createMcpKeyResponse{
		mcpKeyView: toMcpKeyView(created.McpKey),
		Key:        created.Secret,
	})
}

// Revoke handles DELETE /v1/me/mcp-keys/{id}.
func (h *McpKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Invalid key id.")
		return
	}
	if err := h.keys.Revoke(r.Context(), principal.UserID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func toMcpKeyView(key service.McpKey) mcpKeyView {
	view := mcpKeyView{
		ID:        key.ID,
		Name:      key.Name,
		Prefix:    key.Prefix,
		CreatedAt: key.CreatedAt.UTC().Format(time.RFC3339),
	}
	if key.LastUsedAt != nil {
		last := key.LastUsedAt.UTC().Format(time.RFC3339)
		view.LastUsedAt = &last
	}
	return view
}
