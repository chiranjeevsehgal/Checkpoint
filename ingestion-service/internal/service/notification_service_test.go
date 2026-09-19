package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"checkpoint/ingestion/internal/repository"
)

type fakeNotificationRepo struct {
	channel *repository.NotificationChannel
	saved   *repository.NotificationChannel
	err     error
}

func (f *fakeNotificationRepo) GetNotificationChannel(_ context.Context, _ string) (*repository.NotificationChannel, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.channel, nil
}

func (f *fakeNotificationRepo) SetNotificationChannel(_ context.Context, _, topic string, enabled bool) error {
	if f.err != nil {
		return f.err
	}
	f.saved = &repository.NotificationChannel{Enabled: enabled, Topic: topic}
	f.channel = f.saved
	return nil
}

func TestNotificationGetDefaultsToDisabled(t *testing.T) {
	svc := NewNotificationService(&fakeNotificationRepo{})

	got, err := svc.Get(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Enabled || got.Topic != "" {
		t.Fatalf("missing row must be disabled with no topic, got %+v", got)
	}
}

func TestNotificationEnableMintsTopicOnce(t *testing.T) {
	repo := &fakeNotificationRepo{}
	svc := NewNotificationService(repo)

	first, err := svc.Enable(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !first.Enabled || !validTopic(first.Topic) {
		t.Fatalf("first enable produced invalid channel %+v", first)
	}

	second, err := svc.Enable(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if second.Topic != first.Topic {
		t.Fatalf("re-enable rotated the topic: %q -> %q", first.Topic, second.Topic)
	}
}

func TestNotificationDisableClearsTopic(t *testing.T) {
	repo := &fakeNotificationRepo{channel: &repository.NotificationChannel{Enabled: true, Topic: "cp-existing"}}
	svc := NewNotificationService(repo)

	if err := svc.Disable(context.Background(), "user-1"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if repo.saved == nil || repo.saved.Enabled || repo.saved.Topic != "" {
		t.Fatalf("disable must clear topic and flag, got %+v", repo.saved)
	}

	fresh, err := svc.Enable(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("enable after disable: %v", err)
	}
	if !validTopic(fresh.Topic) || fresh.Topic == "cp-existing" {
		t.Fatalf("enable after disable must mint a fresh topic, got %q", fresh.Topic)
	}
}

func TestNotificationEnablePropagatesRepositoryError(t *testing.T) {
	svc := NewNotificationService(&fakeNotificationRepo{err: errors.New("db down")})

	if _, err := svc.Enable(context.Background(), "user-1"); err == nil {
		t.Fatal("expected repository error")
	}
}

func validTopic(topic string) bool {
	if !strings.HasPrefix(topic, ntfyTopicPrefix) || len(topic) > 64 {
		return false
	}
	for _, r := range topic {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !isAlnum {
			return false
		}
	}
	return true
}
