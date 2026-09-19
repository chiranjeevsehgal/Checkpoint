package repository

import "context"

// NotificationChannel is the per-user ntfy subscription. Topic is empty until
// notifications are enabled for the first time.
type NotificationChannel struct {
	Enabled bool
	Topic   string
}

// NotificationRepository persists each user's ntfy channel. The topic is a
// bearer capability generated server-side and never shared across users.
type NotificationRepository interface {
	// GetNotificationChannel returns nil when the user has no row yet.
	GetNotificationChannel(ctx context.Context, userID string) (*NotificationChannel, error)
	SetNotificationChannel(ctx context.Context, userID, topic string, enabled bool) error
}
