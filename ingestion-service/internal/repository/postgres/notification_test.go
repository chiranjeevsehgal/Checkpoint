package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"checkpoint/ingestion/internal/repository"
)

func TestNotificationChannelRoundTrip(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	userID := uuid.NewString()

	t.Cleanup(func() {
		_, _ = p.inner.Exec(context.Background(), `DELETE FROM user_notification_settings WHERE user_id = $1`, userID)
	})

	got, err := p.GetNotificationChannel(ctx, userID)
	if err != nil {
		t.Fatalf("get empty: %v", err)
	}
	if got != nil {
		t.Fatalf("missing row must yield nil, got %+v", got)
	}

	if err := p.SetNotificationChannel(ctx, &repository.NotificationChannel{
		UserID: userID, Topic: "cp-abc", Username: "cp_user", Token: "tk_read", Enabled: true, AdvanceSeconds: 1800,
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err = p.GetNotificationChannel(ctx, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || !got.Enabled || got.Topic != "cp-abc" || got.Username != "cp_user" || got.Token != "tk_read" || got.AdvanceSeconds != 1800 {
		t.Fatalf("got %+v, want enabled cp-abc with credentials and advance", got)
	}

	if err := p.SetNotificationChannel(ctx, &repository.NotificationChannel{UserID: userID}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err = p.GetNotificationChannel(ctx, userID)
	if err != nil {
		t.Fatalf("get disabled: %v", err)
	}
	if got == nil || got.Enabled || got.Topic != "" || got.Username != "" || got.Token != "" {
		t.Fatalf("disable must clear channel, got %+v", got)
	}
}

func TestNotificationChannelIsolatedPerUser(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	owner := uuid.NewString()
	other := uuid.NewString()

	t.Cleanup(func() {
		_, _ = p.inner.Exec(context.Background(), `DELETE FROM user_notification_settings WHERE user_id = ANY($1::uuid[])`, []string{owner, other})
	})

	if err := p.SetNotificationChannel(ctx, &repository.NotificationChannel{
		UserID: owner, Topic: "cp-owner", Enabled: true,
	}); err != nil {
		t.Fatalf("owner set: %v", err)
	}
	got, err := p.GetNotificationChannel(ctx, other)
	if err != nil {
		t.Fatalf("other get: %v", err)
	}
	if got != nil {
		t.Fatalf("other user must not see owner's channel, got %+v", got)
	}
}
