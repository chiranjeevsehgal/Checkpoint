package main

import (
	"encoding/json"
	"testing"

	"transcription-service/internal/model"
)

func TestBuildRetryHandoffEnvelope(t *testing.T) {
	original := json.RawMessage(`{"schema_version":2,"event_id":"e1","data":{"user_id":"u1"}}`)
	evt := buildRetryHandoff("transcription.jobs.v1", "transcribe", "PROVIDER_ERROR", "boom", original)

	if evt.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", evt.SchemaVersion)
	}
	if evt.EventType != model.EventTypeRetryRequested {
		t.Fatalf("event_type = %q, want %q", evt.EventType, model.EventTypeRetryRequested)
	}
	if evt.EventID == "" {
		t.Fatal("event_id must be set")
	}
	if evt.Data.SourceService != "transcription-service" {
		t.Fatalf("source_service = %q", evt.Data.SourceService)
	}
	if evt.Data.SourceTopic != "transcription.jobs.v1" || evt.Data.Stage != "transcribe" ||
		evt.Data.ErrorCode != "PROVIDER_ERROR" || evt.Data.ErrorMessage != "boom" {
		t.Fatalf("unexpected handoff data: %+v", evt.Data)
	}
	if string(evt.Data.OriginalEvent) != string(original) {
		t.Fatalf("original_event = %q, want %q", evt.Data.OriginalEvent, original)
	}
}
