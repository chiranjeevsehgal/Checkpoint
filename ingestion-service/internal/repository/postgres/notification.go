package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"checkpoint/ingestion/internal/repository"
)

// GetNotificationChannel returns the user's ntfy channel, or nil when the user
// has never enabled notifications.
func (p *Pool) GetNotificationChannel(ctx context.Context, userID string) (*repository.NotificationChannel, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var enabled bool
	var topic *string
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT enabled, ntfy_topic FROM user_notification_settings WHERE user_id = $1`,
			userID).Scan(&enabled, &topic)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	channel := &repository.NotificationChannel{Enabled: enabled}
	if topic != nil {
		channel.Topic = *topic
	}
	return channel, nil
}

// SetNotificationChannel upserts the channel. The last write wins; an empty
// topic is stored as NULL so a disabled user exposes no capability.
func (p *Pool) SetNotificationChannel(ctx context.Context, userID, topic string, enabled bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO user_notification_settings (user_id, ntfy_topic, enabled, updated_at)
			VALUES ($1, NULLIF($2, ''), $3, NOW())
			ON CONFLICT (user_id) DO UPDATE
			SET ntfy_topic = EXCLUDED.ntfy_topic,
			    enabled = EXCLUDED.enabled,
			    updated_at = NOW()`, userID, topic, enabled)
		return err
	})
}
