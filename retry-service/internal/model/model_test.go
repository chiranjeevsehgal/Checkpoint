package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func originalEvent(eventID, userID string) json.RawMessage {
	data := map[string]interface{}{"audio_id": uuid.NewString()}
	if userID != "" {
		data["user_id"] = userID
	}
	raw, err := json.Marshal(map[string]interface{}{
		"schema_version": 2,
		"event_id":       eventID,
		"event_type":     "TRANSCRIPTION_REQUESTED",
		"occurred_at":    "2026-01-02T03:04:05Z",
		"data":           data,
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func handoff(eventID, userID string) *RetryRequestedEvent {
	return &RetryRequestedEvent{
		Envelope: Envelope{
			SchemaVersion: SchemaVersion,
			EventID:       uuid.NewString(),
			EventType:     EventTypeRetryRequested,
			OccurredAt:    "2026-01-02T03:04:05Z",
		},
		Data: RetryRequestedData{
			SourceService: "transcription-service",
			SourceTopic:   "transcription.jobs.v1",
			Stage:         "provider_call",
			ErrorCode:     "PROVIDER_5XX",
			ErrorMessage:  "provider returned 502",
			OriginalEvent: originalEvent(eventID, userID),
		},
	}
}

func TestValidateAcceptsHandoffWithUser(t *testing.T) {
	eventID := uuid.NewString()
	userID := uuid.NewString()

	parsed, err := handoff(eventID, userID).Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if parsed.EventID != eventID {
		t.Fatalf("event id: got %s want %s", parsed.EventID, eventID)
	}
	if parsed.UserID != userID {
		t.Fatalf("user id: got %s want %s", parsed.UserID, userID)
	}
}

func TestValidateAcceptsHandoffWithoutUser(t *testing.T) {
	parsed, err := handoff(uuid.NewString(), "").Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if parsed.UserID != "" {
		t.Fatalf("user id should be empty, got %s", parsed.UserID)
	}
}

func TestValidateRejectsWrongSchemaVersion(t *testing.T) {
	e := handoff(uuid.NewString(), uuid.NewString())
	e.SchemaVersion = 3
	if _, err := e.Validate(); err == nil {
		t.Fatal("expected schema_version mismatch to be rejected")
	}
}

func TestValidateRejectsWrongEventType(t *testing.T) {
	e := handoff(uuid.NewString(), uuid.NewString())
	e.EventType = "EXTRACTION_REQUESTED"
	if _, err := e.Validate(); err == nil {
		t.Fatal("expected wrong event_type to be rejected")
	}
}

func TestValidateRejectsMissingSource(t *testing.T) {
	for name, mutate := range map[string] func(*RetryRequestedEvent){
		"source_service": func(e *RetryRequestedEvent) { e.Data.SourceService = " " },
		"source_topic":   func(e *RetryRequestedEvent) { e.Data.SourceTopic = "" },
	} {
		e := handoff(uuid.NewString(), uuid.NewString())
		mutate(e)
		if _, err := e.Validate(); err == nil {
			t.Fatalf("expected missing %s to be rejected", name)
		}
	}
}

func TestValidateRejectsUnparseableOriginal(t *testing.T) {
	e := handoff(uuid.NewString(), uuid.NewString())
	e.Data.OriginalEvent = json.RawMessage("{not json")
	if _, err := e.Validate(); err == nil {
		t.Fatal("expected unparseable original_event to be rejected")
	}
}

func TestValidateRejectsNonUUIDOriginalEventID(t *testing.T) {
	e := handoff("not-a-uuid", uuid.NewString())
	if _, err := e.Validate(); err == nil {
		t.Fatal("expected non-uuid original event_id to be rejected")
	}
}

func TestValidateRejectsNonUUIDOriginalUserID(t *testing.T) {
	e := handoff(uuid.NewString(), "not-a-uuid")
	if _, err := e.Validate(); err == nil {
		t.Fatal("expected non-uuid original user_id to be rejected")
	}
}

func TestRecordOutcomeStrings(t *testing.T) {
	want := map[RecordOutcome]string{
		OutcomeScheduled: "scheduled",
		OutcomeRearmed:   "rearmed",
		OutcomeFailed:    "failed",
		OutcomeDuplicate: "duplicate",
		OutcomeTerminal:  "terminal",
	}
	for outcome, name := range want {
		if outcome.String() != name {
			t.Fatalf("outcome string: got %q want %q", outcome.String(), name)
		}
	}
	if !strings.Contains(RecordOutcome(99).String(), "unknown") {
		t.Fatal("unknown outcome should stringify defensively")
	}
}
