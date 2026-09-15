package http

import (
	"context"
	"net/http"
	"time"

	"checkpoint/ingestion/internal/metrics"
)

// accountService is the subset of service.AccountService used by HTTP.
type accountService interface {
	RequestDeletion(ctx context.Context, userID string) error
}

// AccountHandler serves the account lifecycle API.
type AccountHandler struct {
	accounts  accountService
	requested *metrics.Counter
}

// NewAccountHandler wires the account endpoints.
func NewAccountHandler(accounts accountService, reg *metrics.Registry) *AccountHandler {
	return &AccountHandler{
		accounts:  accounts,
		requested: reg.Counter("account_deletions_requested_total"),
	}
}

// Delete handles DELETE /v1/me. It records a tombstone and acknowledges
// asynchronously; the purge runs in the deletion worker.
func (h *AccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	now := time.Now().UTC()
	if !HasRecentAuth(principal, now) && !HasRecentEmailVerification(principal, now) {
		writeError(w, r, http.StatusForbidden, CodeReauthRequired, "Re-authentication is required for this action.")
		return
	}
	if err := h.accounts.RequestDeletion(r.Context(), principal.UserID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	h.requested.Inc()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
}
