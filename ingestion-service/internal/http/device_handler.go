package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
)

// deviceService is the subset of service.DeviceService used by HTTP.
type deviceService interface {
	Get(ctx context.Context, userID string) (*domain.Device, error)
	IsOwnedBy(ctx context.Context, userID, deviceID string) (bool, error)
	Claim(ctx context.Context, userID, deviceID, cloudClaimSecret string) (*domain.Device, error)
	Release(ctx context.Context, userID string) error
}

// DeviceHandler serves the pendant ownership API.
type DeviceHandler struct {
	devices  deviceService
	claimed  *metrics.Counter
	released *metrics.Counter
}

// NewDeviceHandler wires the device endpoints.
func NewDeviceHandler(devices deviceService, reg *metrics.Registry) *DeviceHandler {
	return &DeviceHandler{
		devices:  devices,
		claimed:  reg.Counter("device_claims_total", "status"),
		released: reg.Counter("device_releases_total"),
	}
}

type deviceResponse struct {
	DeviceID  string  `json:"device_id"`
	State     string  `json:"state"`
	ClaimedAt *string `json:"claimed_at,omitempty"`
}

// Get handles GET /v1/device.
func (h *DeviceHandler) Get(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	device, err := h.devices.Get(r.Context(), principal.UserID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if device == nil {
		writeError(w, r, http.StatusNotFound, CodeDeviceNotFound, "No pendant is linked to this account.")
		return
	}
	writeJSON(w, http.StatusOK, toDeviceResponse(device))
}

type claimRequest struct {
	DeviceID         string `json:"device_id"`
	CloudClaimSecret string `json:"cloud_claim_secret"`
}

// Claim handles POST /v1/device/claim.
func (h *DeviceHandler) Claim(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	raw, ok := readRawBody(w, r, 1<<20)
	if !ok {
		return
	}
	var req claimRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}
	device, err := h.devices.Claim(r.Context(), principal.UserID, req.DeviceID, req.CloudClaimSecret)
	if err != nil {
		h.claimed.Inc("failed")
		writeServiceError(w, r, err)
		return
	}
	h.claimed.Inc("succeeded")
	writeJSON(w, http.StatusOK, toDeviceResponse(device))
}

// Release handles POST /v1/device/release. It requires a freshly
// re-authenticated session.
func (h *DeviceHandler) Release(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	if !HasRecentAuth(principal, time.Now().UTC()) {
		writeError(w, r, http.StatusForbidden, CodeReauthRequired, "Re-authentication is required for this action.")
		return
	}
	if err := h.devices.Release(r.Context(), principal.UserID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	h.released.Inc()
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func toDeviceResponse(device *domain.Device) deviceResponse {
	resp := deviceResponse{DeviceID: device.DeviceID, State: device.State}
	if device.ClaimedAt != nil {
		claimedAt := device.ClaimedAt.UTC().Format(time.RFC3339)
		resp.ClaimedAt = &claimedAt
	}
	return resp
}
