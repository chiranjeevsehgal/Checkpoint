package service

import (
	"context"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

// Settings is the per-user view returned to the API.
type Settings struct {
	Languages []string
	Timezone  string
	Available []domain.Language
}

// SettingsService owns reads and writes of per-user preferences.
type SettingsService struct {
	settings repository.UserSettingsRepository
}

// NewSettingsService wires the settings service.
func NewSettingsService(settings repository.UserSettingsRepository) *SettingsService {
	return &SettingsService{settings: settings}
}

// Get returns the user's preferences plus the language catalog the client
// selects from. A missing row means defaults: no language filtering and the
// service-wide reminder timezone.
func (s *SettingsService) Get(ctx context.Context, userID string) (*Settings, error) {
	languageCodes, err := s.settings.GetLanguages(ctx, userID)
	if err != nil {
		return nil, err
	}
	timezone, err := s.settings.GetTimezone(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Settings{Languages: languageCodes, Timezone: timezone, Available: domain.SupportedLanguages}, nil
}

// SetLanguages validates and persists the selection; an empty set means no
// filtering. It returns the normalized codes that were stored.
func (s *SettingsService) SetLanguages(ctx context.Context, userID string, codes []string) ([]string, error) {
	normalized, err := domain.ValidateLanguages(codes)
	if err != nil {
		return nil, err
	}
	if err := s.settings.SetLanguages(ctx, userID, normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// SetTimezone validates and persists the IANA zone, returning the stored value.
func (s *SettingsService) SetTimezone(ctx context.Context, userID, timezone string) (string, error) {
	normalized, err := domain.ValidateTimezone(timezone)
	if err != nil {
		return "", err
	}
	if err := s.settings.SetTimezone(ctx, userID, normalized); err != nil {
		return "", err
	}
	return normalized, nil
}
