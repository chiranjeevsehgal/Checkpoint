package service

import (
	"context"
	"crypto/sha256"
	"errors"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

// DeviceService validates device input and orchestrates claims.
type DeviceService struct {
	devices repository.DeviceRepository
}

// NewDeviceService wires the device service.
func NewDeviceService(devices repository.DeviceRepository) *DeviceService {
	return &DeviceService{devices: devices}
}

// Get returns the user's owned pendant, or nil when none is claimed.
func (s *DeviceService) Get(ctx context.Context, userID string) (*domain.Device, error) {
	device, err := s.devices.GetByUser(ctx, userID)
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return nil, nil
	}
	return device, err
}

// IsOwnedBy reports whether the device belongs to the user.
func (s *DeviceService) IsOwnedBy(ctx context.Context, userID, deviceID string) (bool, error) {
	normalized, err := domain.NormalizeDeviceID(deviceID)
	if err != nil {
		return false, err
	}
	return s.devices.IsOwnedBy(ctx, userID, normalized)
}

// Claim hashes the cloud secret and assigns a provisioned pendant.
func (s *DeviceService) Claim(ctx context.Context, userID, deviceID, cloudClaimSecret string) (*domain.Device, error) {
	normalizedID, err := domain.NormalizeDeviceID(deviceID)
	if err != nil {
		return nil, err
	}
	secret, err := domain.DecodeClaimSecret(cloudClaimSecret)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(secret)
	if err := s.devices.Claim(ctx, userID, normalizedID, digest[:]); err != nil {
		return nil, err
	}
	return s.devices.GetByUser(ctx, userID)
}

// Release returns the user's own pendant to the unowned pool.
func (s *DeviceService) Release(ctx context.Context, userID string) error {
	return s.devices.Release(ctx, userID)
}
