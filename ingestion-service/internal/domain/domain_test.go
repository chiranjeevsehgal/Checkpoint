package domain

import (
	"errors"
	"testing"
	"time"
)

func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate("clip.ogg", "audio/ogg", 100); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}
	if err := ValidateCreate("CLIP.OGG", "Audio/Ogg", 100); err != nil {
		t.Fatalf("case-insensitive ogg rejected: %v", err)
	}
	if err := ValidateCreate("recording-2026-09-06", "audio/ogg", 100); err != nil {
		t.Fatalf("extensionless ogg filename rejected: %v", err)
	}
	if err := ValidateCreate("  ", "audio/ogg", 100); !errors.Is(err, ErrInvalidFilename) {
		t.Fatalf("expected ErrInvalidFilename, got %v", err)
	}
	if err := ValidateCreate("a.ogg", "audio/ogg", 0); !errors.Is(err, ErrInvalidSize) {
		t.Fatalf("expected ErrInvalidSize, got %v", err)
	}
	if err := ValidateCreate("a.ogg", "audio/ogg", MaxUploadBytes); err != nil {
		t.Fatalf("exactly 10 MB must be accepted: %v", err)
	}
	if err := ValidateCreate("a.ogg", "audio/ogg", MaxUploadBytes+1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
	for _, tc := range []struct{ filename, contentType string }{
		{"meeting.wav", "audio/wav"},
		{"song.mp3", "audio/mpeg"},
		{"clip.webm", "audio/webm"},
		{"a.bin", "application/octet-stream"},
		{"a.ogg", ""},
	} {
		if err := ValidateCreate(tc.filename, tc.contentType, 100); !errors.Is(err, ErrUnsupportedMediaType) {
			t.Fatalf("%q/%q: expected ErrUnsupportedMediaType, got %v", tc.filename, tc.contentType, err)
		}
	}
}

func TestNormalizeCreate(t *testing.T) {
	fn, ct := NormalizeCreate("  Meeting.OGG  ", "  Audio/OGG  ")
	if fn != "Meeting.OGG" || ct != "audio/ogg" {
		t.Fatalf("got %q/%q", fn, ct)
	}
}

func TestValidateFilenameLimits(t *testing.T) {
	s := ""
	for i := 0; i < 300; i++ {
		s += "a"
	}
	if err := ValidateCreate(s+".ogg", "audio/ogg", 100); !errors.Is(err, ErrInvalidFilename) {
		t.Fatalf("long filename must fail, got %v", err)
	}
	if err := ValidateIdempotencyKey(s + s); !errors.Is(err, ErrInvalidSize) {
		t.Fatalf("long key must fail, got %v", err)
	}
}

func TestValidateChecksumFormat(t *testing.T) {
	if err := ValidateChecksumFormat(""); err != nil {
		t.Fatalf("empty checksum is allowed (absent): %v", err)
	}
	valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := ValidateChecksumFormat(valid); err != nil {
		t.Fatalf("valid checksum rejected: %v", err)
	}
	for _, bad := range []string{"xyz", "0123", valid[:63], valid + "00", "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"} {
		if err := ValidateChecksumFormat(bad); !errors.Is(err, ErrInvalidChecksum) {
			t.Fatalf("%q: want ErrInvalidChecksum, got %v", bad, err)
		}
	}
}

func TestParseRecordedAt(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	for _, raw := range []string{"", "   "} {
		got, err := ParseRecordedAt(raw, now)
		if err != nil || got != nil {
			t.Fatalf("empty %q must yield nil,nil; got %v,%v", raw, got, err)
		}
	}

	got, err := ParseRecordedAt("2026-09-13T10:15:00+05:30", now)
	if err != nil {
		t.Fatalf("valid recorded_at rejected: %v", err)
	}
	if want := time.Date(2026, 9, 13, 4, 45, 0, 0, time.UTC); got == nil || !got.Equal(want) {
		t.Fatalf("got %v, want %v UTC", got, want)
	}

	if _, err := ParseRecordedAt("2026-09-14T11:00:00Z", now); err != nil {
		t.Fatalf("within future skew must be accepted: %v", err)
	}
	for _, bad := range []string{"not-a-date", "2026-09-15T00:00:00Z"} {
		if _, err := ParseRecordedAt(bad, now); !errors.Is(err, ErrInvalidRecordedAt) {
			t.Fatalf("%q: want ErrInvalidRecordedAt, got %v", bad, err)
		}
	}
}

func TestCompletionStates(t *testing.T) {
	if !CanTransitionToComplete(StatusUploading) {
		t.Fatal("UPLOADING must allow completion")
	}
	for _, s := range []string{StatusReady, StatusSubmitted, StatusExpired, StatusRejected} {
		if CanTransitionToComplete(s) {
			t.Fatalf("%s must not allow completion", s)
		}
	}
	if !IsCompletionIdempotent(StatusReady) || !IsCompletionIdempotent(StatusSubmitted) {
		t.Fatal("READY and SUBMITTED must be idempotent successes")
	}
	if IsCompletionIdempotent(StatusUploading) || IsCompletionIdempotent(StatusExpired) {
		t.Fatal("UPLOADING and EXPIRED must not be idempotent successes")
	}
}

func TestObjectKeyFor(t *testing.T) {
	key := ObjectKeyFor("user-1", "upload-1", time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC))
	want := "user-1/2026/09/06/upload-1"
	if key != want {
		t.Fatalf("got %q, want %q", key, want)
	}
}

func TestNextRetryDelay(t *testing.T) {
	cases := map[int]time.Duration{
		1:  5 * time.Second,
		2:  15 * time.Second,
		3:  time.Minute,
		4:  5 * time.Minute,
		50: 5 * time.Minute,
	}
	for attempt, want := range cases {
		if got := NextRetryDelay(attempt); got != want {
			t.Fatalf("attempt %d: got %v, want %v", attempt, got, want)
		}
	}
}
