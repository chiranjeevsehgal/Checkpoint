package http

import (
	"context"
	"net/http"
)

// sessionRevoker invalidates an identity's sessions other than the current one.
type sessionRevoker interface {
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID string) error
}

// SessionHandler serves session management for the account API.
type SessionHandler struct {
	sessions sessionRevoker
}

// NewSessionHandler wires the session endpoints.
func NewSessionHandler(sessions sessionRevoker) *SessionHandler {
	return &SessionHandler{sessions: sessions}
}

// Delete handles DELETE /v1/me/sessions by revoking every other session for
// the caller. To sign out everywhere the client still signs out locally.
func (h *SessionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	if h.sessions == nil {
		writeError(w, r, http.StatusServiceUnavailable, CodeAuthUnavailable, "Session management is temporarily unavailable.")
		return
	}
	if err := h.sessions.RevokeOtherSessions(r.Context(), principal.UserID, principal.SessionID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
