package service

import (
	"context"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

// Settings is the per-user view returned to the API.
type Settings struct {
	Languages []string
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

// Get returns the user's languages plus the catalog the client selects from.
// A missing row means the user has not chosen, so the default is no filtering.
func (s *SettingsService) Get(ctx context.Context, userID string) (*Settings, error) {
	languageCodes, err := s.settings.GetLanguages(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Settings{Languages: languageCodes, Available: domain.SupportedLanguages}, nil
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
