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
	var topic, username, token *string
	var advance *int
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT enabled, ntfy_topic, ntfy_username, ntfy_token, advance_seconds
			FROM user_notification_settings WHERE user_id = $1`, userID).
			Scan(&enabled, &topic, &username, &token, &advance)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	channel := &repository.NotificationChannel{UserID: userID, Enabled: enabled}
	if topic != nil {
		channel.Topic = *topic
	}
	if username != nil {
		channel.Username = *username
	}
	if token != nil {
		channel.Token = *token
	}
	if advance != nil {
		channel.AdvanceSeconds = *advance
	}
	return channel, nil
}

// SetNotificationChannel upserts the channel. The last write wins; empty
// fields are stored as NULL so a disabled user exposes no capability.
func (p *Pool) SetNotificationChannel(ctx context.Context, channel *repository.NotificationChannel) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, channel.UserID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO user_notification_settings
				(user_id, ntfy_topic, ntfy_username, ntfy_token, enabled, advance_seconds, updated_at)
			VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), $5, NULLIF($6, 0), NOW())
			ON CONFLICT (user_id) DO UPDATE
			SET ntfy_topic = EXCLUDED.ntfy_topic,
			    ntfy_username = EXCLUDED.ntfy_username,
			    ntfy_token = EXCLUDED.ntfy_token,
			    enabled = EXCLUDED.enabled,
			    advance_seconds = EXCLUDED.advance_seconds,
			    updated_at = NOW()`,
			channel.UserID, channel.Topic, channel.Username, channel.Token, channel.Enabled, channel.AdvanceSeconds)
		return err
	})
}
