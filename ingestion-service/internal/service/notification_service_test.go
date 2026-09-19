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

func (f *fakeNotificationRepo) SetNotificationChannel(_ context.Context, channel *repository.NotificationChannel) error {
	if f.err != nil {
		return f.err
	}
	saved := *channel
	f.saved = &saved
	f.channel = &saved
	return nil
}

type fakeProvisioner struct {
	ensured   []string
	granted   []string
	revoked   []string
	deleted   []string
	token     string
	mintErr   error
	ensureErr error
}

func (f *fakeProvisioner) EnsureUser(_ context.Context, username, _ string) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	f.ensured = append(f.ensured, username)
	return nil
}

func (f *fakeProvisioner) GrantRead(_ context.Context, username, topic string) error {
	f.granted = append(f.granted, username+"/"+topic)
	return nil
}

func (f *fakeProvisioner) RevokeAccess(_ context.Context, username, topic string) error {
	f.revoked = append(f.revoked, username+"/"+topic)
	return nil
}

func (f *fakeProvisioner) DeleteUser(_ context.Context, username string) error {
	f.deleted = append(f.deleted, username)
	return nil
}

func (f *fakeProvisioner) MintToken(_ context.Context, _, _ string) (string, error) {
	if f.mintErr != nil {
		return "", f.mintErr
	}
	if f.token == "" {
		return "tk_minted", nil
	}
	return f.token, nil
}

func TestNotificationGetDefaultsToDisabled(t *testing.T) {
	svc := NewNotificationService(&fakeNotificationRepo{}, nil)

	got, err := svc.Get(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Enabled || got.Topic != "" || got.Token != "" {
		t.Fatalf("missing row must be disabled with no topic, got %+v", got)
	}
}

func TestNotificationEnableMintsTopicOnce(t *testing.T) {
	repo := &fakeNotificationRepo{}
	svc := NewNotificationService(repo, nil)

	first, err := svc.Enable(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !first.Enabled || !validTopic(first.Topic) || first.Token != "" {
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

func TestNotificationEnableProvisionsReadToken(t *testing.T) {
	repo := &fakeNotificationRepo{}
	provisioner := &fakeProvisioner{token: "tk_read"}
	svc := NewNotificationService(repo, provisioner)

	got, err := svc.Enable(context.Background(), "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if got.Token != "tk_read" {
		t.Fatalf("token = %q, want tk_read", got.Token)
	}
	if len(provisioner.ensured) != 1 || provisioner.ensured[0] != "cp_11111111222233334444555555555555" {
		t.Fatalf("ensured = %v", provisioner.ensured)
	}
	if len(provisioner.granted) != 1 || !strings.HasSuffix(provisioner.granted[0], "/"+got.Topic) {
		t.Fatalf("granted = %v, topic = %q", provisioner.granted, got.Topic)
	}
	if repo.saved == nil || repo.saved.Token != "tk_read" || repo.saved.Username == "" {
		t.Fatalf("credentials not persisted: %+v", repo.saved)
	}
}

func TestNotificationEnableReusesStoredToken(t *testing.T) {
	repo := &fakeNotificationRepo{channel: &repository.NotificationChannel{
		Enabled: true, Topic: "cp-existing", Username: "cp_user", Token: "tk_stored",
	}}
	provisioner := &fakeProvisioner{}
	svc := NewNotificationService(repo, provisioner)

	got, err := svc.Enable(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if got.Token != "tk_stored" || got.Topic != "cp-existing" {
		t.Fatalf("reused channel wrong: %+v", got)
	}
	if len(provisioner.ensured) != 0 || len(provisioner.granted) != 0 {
		t.Fatalf("existing token must not re-provision: %+v", provisioner)
	}
}

func TestNotificationEnableProvisionFailureDoesNotPersist(t *testing.T) {
	repo := &fakeNotificationRepo{}
	provisioner := &fakeProvisioner{mintErr: errors.New("ntfy down")}
	svc := NewNotificationService(repo, provisioner)

	if _, err := svc.Enable(context.Background(), "user-1"); err == nil {
		t.Fatal("expected provisioning error")
	}
	if repo.saved != nil {
		t.Fatalf("failed provisioning must not persist, got %+v", repo.saved)
	}
}

func TestNotificationDisableClearsAndRevokes(t *testing.T) {
	repo := &fakeNotificationRepo{channel: &repository.NotificationChannel{
		Enabled: true, Topic: "cp-existing", Username: "cp_user", Token: "tk_stored",
	}}
	provisioner := &fakeProvisioner{}
	svc := NewNotificationService(repo, provisioner)

	if err := svc.Disable(context.Background(), "user-1"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if repo.saved == nil || repo.saved.Enabled || repo.saved.Topic != "" || repo.saved.Token != "" {
		t.Fatalf("disable must clear topic and token, got %+v", repo.saved)
	}
	if len(provisioner.revoked) != 1 || len(provisioner.deleted) != 1 {
		t.Fatalf("remote cleanup missing: revoked=%v deleted=%v", provisioner.revoked, provisioner.deleted)
	}
}

func TestNotificationEnablePropagatesRepositoryError(t *testing.T) {
	svc := NewNotificationService(&fakeNotificationRepo{err: errors.New("db down")}, nil)

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
