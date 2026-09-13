package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewAudioReadyPayloadTranscription(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 15, 30, 0, time.UTC)
	size := int64(1024)
	u := &Upload{
		ID: "audio-1", UserID: "user-1", Bucket: "audio",
		ObjectKey: "user-1/2026/09/06/audio-1", ContentType: "audio/ogg",
		ExpectedSize: &size,
	}
	got := NewAudioReadyPayload("event-1", u, 184320, "abc123", now)
	if got.SchemaVersion != SchemaVersion || SchemaVersion != 3 {
		t.Fatalf("schema version must be 3, got %+v", got)
	}
	if got.EventType != EventTranscriptionRequested {
		t.Fatalf("event type must be %q, got %q", EventTranscriptionRequested, got.EventType)
	}
	if got.Data.AudioID != "audio-1" || got.Data.UserID != "user-1" {
		t.Fatalf("identity fields wrong: %+v", got.Data)
	}
	if got.Data.Bucket != "audio" || got.Data.ObjectKey != "user-1/2026/09/06/audio-1" {
		t.Fatalf("by-reference fields wrong: %+v", got.Data)
	}
	if got.Data.SizeBytes != 184320 || got.Data.ChecksumSHA256 != "abc123" {
		t.Fatalf("size/checksum wrong: %+v", got.Data)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioReadyPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Data.UserID != "user-1" {
		t.Fatalf("round-trip lost user_id: %+v", decoded)
	}

	// Empty checksum must be omitted so absent stays absent.
	noSum := NewAudioReadyPayload("event-2", u, 100, "", now)
	raw, _ = json.Marshal(noSum)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	data := m["data"].(map[string]any)
	if _, ok := data["checksum_sha256"]; ok {
		t.Fatalf("empty checksum must be omitted, got %v", data)
	}
	if _, ok := data["recorded_at"]; ok {
		t.Fatalf("absent recorded_at must be omitted, got %v", data)
	}
}

func TestNewAudioReadyPayloadRecordedAt(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 15, 30, 0, time.UTC)
	recorded := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	size := int64(1024)
	u := &Upload{
		ID: "audio-1", UserID: "user-1", Bucket: "audio",
		ObjectKey: "user-1/2026/09/06/audio-1", ContentType: "audio/ogg",
		ExpectedSize: &size, RecordedAt: &recorded,
	}

	got := NewAudioReadyPayload("event-1", u, 184320, "abc123", now)
	if got.Data.RecordedAt != "2026-09-06T09:00:00Z" {
		t.Fatalf("recorded_at = %q, want 2026-09-06T09:00:00Z", got.Data.RecordedAt)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	data := m["data"].(map[string]any)
	if data["recorded_at"] != "2026-09-06T09:00:00Z" {
		t.Fatalf("round-trip lost recorded_at: %v", data)
	}
}
