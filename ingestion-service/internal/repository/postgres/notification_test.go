package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
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

	if err := p.SetNotificationChannel(ctx, userID, "cp-abc", true); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err = p.GetNotificationChannel(ctx, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || !got.Enabled || got.Topic != "cp-abc" {
		t.Fatalf("got %+v, want enabled cp-abc", got)
	}

	if err := p.SetNotificationChannel(ctx, userID, "", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err = p.GetNotificationChannel(ctx, userID)
	if err != nil {
		t.Fatalf("get disabled: %v", err)
	}
	if got == nil || got.Enabled || got.Topic != "" {
		t.Fatalf("disable must clear topic, got %+v", got)
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

	if err := p.SetNotificationChannel(ctx, owner, "cp-owner", true); err != nil {
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
