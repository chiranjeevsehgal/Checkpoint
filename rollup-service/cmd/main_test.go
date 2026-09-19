package main

import (
	"testing"
	"time"
)

func TestActivitySince(t *testing.T) {
	// 2026-09-21 is a Monday; 2026-09-23 is a Wednesday.
	monday := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	wednesday := time.Date(2026, 9, 23, 3, 0, 0, 0, time.UTC)

	cases := []struct {
		name         string
		now          time.Time
		lookbackDays int
		want         time.Time
	}{
		{"monday widens to cover the week", monday, 0, time.Date(2026, 9, 12, 3, 0, 0, 0, time.UTC)},
		{"weekday uses the daily window", wednesday, 0, time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)},
		{"larger lookback wins over monday", monday, 10, time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := activitySince(c.now, time.UTC, c.lookbackDays); !got.Equal(c.want) {
				t.Fatalf("activitySince = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMondayOfPreviousWeek(t *testing.T) {
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC), time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC), time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		if got := mondayOfPreviousWeek(c.now); !got.Equal(c.want) {
			t.Fatalf("mondayOfPreviousWeek(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}

func TestRunRetryDelayIsCapped(t *testing.T) {
	if got := runRetryDelay(1); got != time.Second {
		t.Fatalf("runRetryDelay(1) = %v, want 1s", got)
	}
	if got := runRetryDelay(10); got != 30*time.Second {
		t.Fatalf("runRetryDelay(10) = %v, want 30s", got)
	}
}
