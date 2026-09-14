package domain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Device lifecycle states. reset_required means the hardware must be
// recovered by a trusted operator before it can be claimed again.
const (
	DeviceStateUnowned       = "unowned"
	DeviceStateOwned         = "owned"
	DeviceStateResetRequired = "reset_required"
)

// DeviceIDHexLen is the device id length emitted by the firmware.
const DeviceIDHexLen = 32

// ClaimSecretBytes is the length of the 256-bit cloud claim secret.
const ClaimSecretBytes = 32

var (
	ErrInvalidDeviceID  = errors.New("device_id must be 32 lowercase hex characters")
	ErrInvalidClaimSecret = errors.New("cloud_claim_secret must be 64 lowercase hex characters")
)

// Device is a pendant's cloud ownership record.
type Device struct {
	DeviceID  string
	UserID    *string
	State     string
	ClaimedAt *time.Time
	UpdatedAt time.Time
}

// NormalizeDeviceID accepts a 32-character hex device id in any case and
// returns its canonical lowercase form.
func NormalizeDeviceID(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if len(v) != DeviceIDHexLen {
		return "", ErrInvalidDeviceID
	}
	if _, err := hex.DecodeString(v); err != nil {
		return "", ErrInvalidDeviceID
	}
	return v, nil
}

// DecodeClaimSecret decodes the 32-byte cloud claim secret from hex.
func DecodeClaimSecret(raw string) ([]byte, error) {
	v := strings.TrimSpace(raw)
	if len(v) != ClaimSecretBytes*2 {
		return nil, ErrInvalidClaimSecret
	}
	decoded, err := hex.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidClaimSecret, err)
	}
	return decoded, nil
}
