package main

import (
	"testing"
	"time"
)

func TestValidDeviceState(t *testing.T) {
	for _, state := range []string{"unowned", "owned", "reset_required"} {
		if !validDeviceState(state) {
			t.Fatalf("expected %q to be valid", state)
		}
	}
	for _, state := range []string{"", "OWNED", "pending", "deleted"} {
		if validDeviceState(state) {
			t.Fatalf("expected %q to be invalid", state)
		}
	}
}

func TestFormatTimePtr(t *testing.T) {
	if got := formatTimePtr(nil); got != nil {
		t.Fatalf("nil time must format to nil, got %v", *got)
	}
	moment := time.Date(2026, time.September, 15, 18, 13, 52, 0, time.UTC)
	got := formatTimePtr(&moment)
	if got == nil || *got != "2026-09-15T18:13:52Z" {
		t.Fatalf("unexpected format: %v", got)
	}
}
