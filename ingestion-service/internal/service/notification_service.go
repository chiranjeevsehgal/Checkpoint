package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"log/slog"
	"strings"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

// ntfy topics allow only [-_A-Za-z0-9] and cap at 64 chars.
const (
	ntfyTopicPrefix = "cp-"
	topicBytes      = 20 // 160 bits; base32 keeps the result inside ntfy's charset
)

// NotificationProvisioner provisions per-user ntfy read access. A nil
// provisioner keeps the anonymous read-only fallback for environments that do
// not configure ntfy authentication.
type NotificationProvisioner interface {
	EnsureUser(ctx context.Context, username, password string) error
	GrantRead(ctx context.Context, username, topic string) error
	RevokeAccess(ctx context.Context, username, topic string) error
	DeleteUser(ctx context.Context, username string) error
	MintToken(ctx context.Context, username, password string) (string, error)
}

// NotificationChannel is the per-user ntfy subscription returned to the API.
type NotificationChannel struct {
	Enabled        bool
	Topic          string
	Token          string
	AdvanceMinutes int
}

// NotificationService owns reads and writes of the per-user ntfy channel.
type NotificationService struct {
	channels    repository.NotificationRepository
	provisioner NotificationProvisioner
}

// NewNotificationService wires the notification service. provisioner may be nil.
func NewNotificationService(channels repository.NotificationRepository, provisioner NotificationProvisioner) *NotificationService {
	return &NotificationService{channels: channels, provisioner: provisioner}
}

// Get returns the user's channel; a missing row is disabled with no topic.
func (s *NotificationService) Get(ctx context.Context, userID string) (*NotificationChannel, error) {
	channel, err := s.channels.GetNotificationChannel(ctx, userID)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return &NotificationChannel{AdvanceMinutes: domain.DefaultAdvanceMinutes}, nil
	}
	return &NotificationChannel{
		Enabled:        channel.Enabled,
		Topic:          channel.Topic,
		Token:          channel.Token,
		AdvanceMinutes: domain.AdvanceMinutesFromSeconds(channel.AdvanceSeconds),
	}, nil
}

// Enable turns notifications on, minting a topic and (when configured) a
// dedicated ntfy read token only when the user has none. Repeat calls keep the
// existing credentials so subscribed devices stay working.
func (s *NotificationService) Enable(ctx context.Context, userID string) (*NotificationChannel, error) {
	existing, err := s.channels.GetNotificationChannel(ctx, userID)
	if err != nil {
		return nil, err
	}

	channel := &repository.NotificationChannel{UserID: userID}
	if existing != nil {
		channel.Topic = existing.Topic
		channel.Username = existing.Username
		channel.Token = existing.Token
		channel.AdvanceSeconds = existing.AdvanceSeconds
	}
	if channel.Topic == "" {
		if channel.Topic, err = generateTopic(); err != nil {
			return nil, err
		}
	}
	if s.provisioner != nil && channel.Token == "" {
		if err := s.provision(ctx, userID, channel); err != nil {
			return nil, err
		}
	}
	channel.Enabled = true
	if err := s.channels.SetNotificationChannel(ctx, channel); err != nil {
		return nil, err
	}
	return &NotificationChannel{
		Enabled:        true,
		Topic:          channel.Topic,
		Token:          channel.Token,
		AdvanceMinutes: domain.AdvanceMinutesFromSeconds(channel.AdvanceSeconds),
	}, nil
}

// Disable clears the topic and credentials so old subscribers stop receiving;
// a later Enable mints fresh ones. Remote ntfy cleanup is best-effort.
func (s *NotificationService) Disable(ctx context.Context, userID string) error {
	channel, err := s.channels.GetNotificationChannel(ctx, userID)
	if err != nil {
		return err
	}
	if channel == nil {
		return nil
	}
	if s.provisioner != nil && channel.Username != "" {
		if err := s.provisioner.RevokeAccess(ctx, channel.Username, channel.Topic); err != nil {
			slog.Warn("ntfy_access_revoke_failed", "user_id", userID, "error", err)
		}
		if err := s.provisioner.DeleteUser(ctx, channel.Username); err != nil {
			slog.Warn("ntfy_user_delete_failed", "user_id", userID, "error", err)
		}
	}
	return s.channels.SetNotificationChannel(ctx, &repository.NotificationChannel{
		UserID:         userID,
		Enabled:        false,
		AdvanceSeconds: channel.AdvanceSeconds,
	})
}

// SetAdvance stores the user's advance lead in minutes, keeping the channel's
// topic and credentials. It works whether or not notifications are enabled.
func (s *NotificationService) SetAdvance(ctx context.Context, userID string, minutes int) (*NotificationChannel, error) {
	seconds, err := domain.ValidateAdvanceMinutes(minutes)
	if err != nil {
		return nil, err
	}
	existing, err := s.channels.GetNotificationChannel(ctx, userID)
	if err != nil {
		return nil, err
	}

	channel := &repository.NotificationChannel{UserID: userID, AdvanceSeconds: seconds}
	if existing != nil {
		channel.Enabled = existing.Enabled
		channel.Topic = existing.Topic
		channel.Username = existing.Username
		channel.Token = existing.Token
	}
	if err := s.channels.SetNotificationChannel(ctx, channel); err != nil {
		return nil, err
	}
	return &NotificationChannel{
		Enabled:        channel.Enabled,
		Topic:          channel.Topic,
		Token:          channel.Token,
		AdvanceMinutes: domain.AdvanceMinutesFromSeconds(channel.AdvanceSeconds),
	}, nil
}

// provision creates the ntfy user, grants read-only access to the topic, and
// mints its token. The random password is used once and never persisted.
func (s *NotificationService) provision(ctx context.Context, userID string, channel *repository.NotificationChannel) error {
	username := channel.Username
	if username == "" {
		username = ntfyUsername(userID)
	}
	password, err := randomPassword()
	if err != nil {
		return err
	}
	if err := s.provisioner.EnsureUser(ctx, username, password); err != nil {
		return err
	}
	if err := s.provisioner.GrantRead(ctx, username, channel.Topic); err != nil {
		return err
	}
	token, err := s.provisioner.MintToken(ctx, username, password)
	if err != nil {
		return err
	}
	channel.Username = username
	channel.Token = token
	return nil
}

// generateTopic returns a random capability such as "cp-<base32>".
func generateTopic() (string, error) {
	raw := make([]byte, topicBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	return ntfyTopicPrefix + strings.ToLower(encoded), nil
}

// ntfyUsername maps a user id to a stable, ntfy-valid username. Dashes are
// dropped so a UUID yields a compact cp_<32hex>.
func ntfyUsername(userID string) string {
	var b strings.Builder
	for _, r := range userID {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '.' || r == '+' || r == '@'
		if allowed {
			b.WriteRune(r)
		}
	}
	return "cp_" + b.String()
}

func randomPassword() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
