package repository

import "context"

// UserSettingsRepository persists per-user preferences. Languages are stored
// as an ISO-639-3 set and default to empty (no filtering). Timezone is an
// IANA zone id and defaults to empty (fall back to the service default).
type UserSettingsRepository interface {
	GetLanguages(ctx context.Context, userID string) ([]string, error)
	SetLanguages(ctx context.Context, userID string, languages []string) error
	GetTimezone(ctx context.Context, userID string) (string, error)
	SetTimezone(ctx context.Context, userID string, timezone string) error
}
