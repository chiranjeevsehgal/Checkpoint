package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"strings"

	"checkpoint/ingestion/internal/repository"
)

// ntfy topics allow only [-_A-Za-z0-9] and cap at 64 chars.
const (
	ntfyTopicPrefix = "cp-"
	topicBytes      = 20 // 160 bits; base32 keeps the result inside ntfy's charset
)

// NotificationChannel is the per-user ntfy subscription returned to the API.
type NotificationChannel struct {
	Enabled bool
	Topic   string
}

// NotificationService owns reads and writes of the per-user ntfy channel.
type NotificationService struct {
	channels repository.NotificationRepository
}

// NewNotificationService wires the notification service.
func NewNotificationService(channels repository.NotificationRepository) *NotificationService {
	return &NotificationService{channels: channels}
}

// Get returns the user's channel; a missing row is disabled with no topic.
func (s *NotificationService) Get(ctx context.Context, userID string) (*NotificationChannel, error) {
	channel, err := s.channels.GetNotificationChannel(ctx, userID)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return &NotificationChannel{}, nil
	}
	return &NotificationChannel{Enabled: channel.Enabled, Topic: channel.Topic}, nil
}

// Enable turns notifications on, minting a topic only when the user has none.
// Repeat calls keep the existing topic so subscribed devices stay working.
func (s *NotificationService) Enable(ctx context.Context, userID string) (*NotificationChannel, error) {
	topic := ""
	channel, err := s.channels.GetNotificationChannel(ctx, userID)
	if err != nil {
		return nil, err
	}
	if channel != nil {
		topic = channel.Topic
	}
	if topic == "" {
		topic, err = generateTopic()
		if err != nil {
			return nil, err
		}
	}
	if err := s.channels.SetNotificationChannel(ctx, userID, topic, true); err != nil {
		return nil, err
	}
	return &NotificationChannel{Enabled: true, Topic: topic}, nil
}

// Disable clears the topic so old subscribers stop receiving; a later Enable
// mints a fresh one.
func (s *NotificationService) Disable(ctx context.Context, userID string) error {
	return s.channels.SetNotificationChannel(ctx, userID, "", false)
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
