package repository

import (
	"context"

	"checkpoint/ingestion/internal/domain"
)

// DeviceRepository owns pendant ownership reads and the atomic
// claim/release transitions.
type DeviceRepository interface {
	// GetByUser returns the user's owned pendant, or ErrDeviceNotFound.
	GetByUser(ctx context.Context, userID string) (*domain.Device, error)
	// IsOwnedBy reports whether the device is currently owned by userID.
	IsOwnedBy(ctx context.Context, userID, deviceID string) (bool, error)
	// Claim assigns an unowned device to userID when claimHash matches.
	Claim(ctx context.Context, userID, deviceID string, claimHash []byte) error
	// Release returns the user's own pendant to the unowned pool.
	Release(ctx context.Context, userID string) error
}
