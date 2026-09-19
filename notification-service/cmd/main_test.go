package main

import (
	"testing"
	"time"

	"notification-service/internal/model"
)

func TestMessageAdvanceUsesCandidateLead(t *testing.T) {
	c := model.Candidate{Kind: model.KindAdvance, ReminderText: "Call Dad", AdvanceSeconds: 300}
	title, body := message(c, 15*time.Minute)
	if title != "Upcoming reminder" || body != "Call Dad in 5 minutes" {
		t.Fatalf("got %q / %q", title, body)
	}
}

func TestMessageAdvanceFallsBackToDefault(t *testing.T) {
	c := model.Candidate{Kind: model.KindAdvance, ReminderText: "Call Dad"}
	_, body := message(c, 15*time.Minute)
	if body != "Call Dad in 15 minutes" {
		t.Fatalf("body = %q, want Call Dad in 15 minutes", body)
	}
}

func TestMessageDueUnchanged(t *testing.T) {
	c := model.Candidate{Kind: model.KindDue, ReminderText: "Call Dad"}
	title, body := message(c, 15*time.Minute)
	if title != "Reminder" || body != "Call Dad" {
		t.Fatalf("got %q / %q", title, body)
	}
}
