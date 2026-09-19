package repository

import "context"

// NotificationChannel is the per-user ntfy subscription. Topic is empty until
// notifications are enabled. When per-user auth is configured, Username and
// Token identify the dedicated ntfy user with read-only access to Topic.
// AdvanceSeconds is the advance lead; 0 means unset (worker default).
type NotificationChannel struct {
	UserID         string
	Enabled        bool
	Topic          string
	Username       string
	Token          string
	AdvanceSeconds int
}

// NotificationRepository persists each user's ntfy channel. The topic is a
// bearer capability generated server-side and never shared across users.
type NotificationRepository interface {
	// GetNotificationChannel returns nil when the user has no row yet.
	GetNotificationChannel(ctx context.Context, userID string) (*NotificationChannel, error)
	SetNotificationChannel(ctx context.Context, channel *NotificationChannel) error
}
