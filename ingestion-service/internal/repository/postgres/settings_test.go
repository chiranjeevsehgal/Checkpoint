package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestUserSettingsRoundTrip(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	userID := uuid.NewString()

	t.Cleanup(func() {
		_, _ = p.inner.Exec(context.Background(), `DELETE FROM user_settings WHERE user_id = $1`, userID)
	})

	got, err := p.GetLanguages(ctx, userID)
	if err != nil {
		t.Fatalf("get empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing row must yield no languages, got %v", got)
	}

	if err := p.SetLanguages(ctx, userID, []string{"eng", "hin"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err = p.GetLanguages(ctx, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 || got[0] != "eng" || got[1] != "hin" {
		t.Fatalf("got %v, want [eng hin]", got)
	}

	if err := p.SetLanguages(ctx, userID, []string{"fra"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err = p.GetLanguages(ctx, userID)
	if err != nil {
		t.Fatalf("get replace: %v", err)
	}
	if len(got) != 1 || got[0] != "fra" {
		t.Fatalf("got %v, want [fra]", got)
	}
}

func TestUserSettingsTimezoneRoundTrip(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	userID := uuid.NewString()

	t.Cleanup(func() {
		_, _ = p.inner.Exec(context.Background(), `DELETE FROM user_settings WHERE user_id = $1`, userID)
	})

	got, err := p.GetTimezone(ctx, userID)
	if err != nil {
		t.Fatalf("get empty: %v", err)
	}
	if got != "" {
		t.Fatalf("missing row must yield empty timezone, got %q", got)
	}

	if err := p.SetTimezone(ctx, userID, "Europe/Berlin"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := p.SetLanguages(ctx, userID, []string{"eng"}); err != nil {
		t.Fatalf("set languages: %v", err)
	}

	got, err = p.GetTimezone(ctx, userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "Europe/Berlin" {
		t.Fatalf("timezone = %q, want Europe/Berlin", got)
	}
	languages, err := p.GetLanguages(ctx, userID)
	if err != nil {
		t.Fatalf("get languages: %v", err)
	}
	if len(languages) != 1 || languages[0] != "eng" {
		t.Fatalf("setting timezone clobbered languages: %v", languages)
	}
}
