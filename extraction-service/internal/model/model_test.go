package model

import (
	"encoding/json"
	"testing"
)

func validEvent() ExtractionJobRequestedEvent {
	return ExtractionJobRequestedEvent{
		Envelope: Envelope{
			SchemaVersion: 2,
			EventID:       "evt-1",
			EventType:     EventTypeExtractionRequested,
			OccurredAt:    "2026-09-14T10:00:00.000Z",
		},
		Data: ExtractionJobData{
			AudioID: "a5a3a0ae-1e56-4cc0-8aae-6d38170c2b31",
			UserID:  "df38ec6e-cb33-4f10-b5e2-4d24c17e4c26",
			Text:    "please send the report",
		},
	}
}

func TestValidateAcceptsValidEvent(t *testing.T) {
	e := validEvent()
	if err := e.Validate(); err != nil {
		t.Fatalf("expected valid event to pass, got %v", err)
	}
}

func TestValidateRejectsPoison(t *testing.T) {
	cases := map[string]func(*ExtractionJobRequestedEvent){
		"wrong schema_version":  func(e *ExtractionJobRequestedEvent) { e.SchemaVersion = 1 },
		"wrong event_type":      func(e *ExtractionJobRequestedEvent) { e.EventType = "SOMETHING_ELSE" },
		"empty audio_id":        func(e *ExtractionJobRequestedEvent) { e.Data.AudioID = "" },
		"non-uuid audio_id":     func(e *ExtractionJobRequestedEvent) { e.Data.AudioID = "audio-1" },
		"empty user_id":         func(e *ExtractionJobRequestedEvent) { e.Data.UserID = "" },
		"non-uuid user_id":      func(e *ExtractionJobRequestedEvent) { e.Data.UserID = "user-1" },
		"missing text":          func(e *ExtractionJobRequestedEvent) { e.Data.Text = "" },
		"whitespace text":       func(e *ExtractionJobRequestedEvent) { e.Data.Text = "\n\t " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := validEvent()
			mutate(&e)
			err := e.Validate()
			if err == nil {
				t.Fatalf("expected %s to be rejected", name)
			}
			if _, ok := err.(*InvalidEvent); !ok {
				t.Fatalf("expected *InvalidEvent, got %T", err)
			}
		})
	}
}

func TestUnmarshalMatchesTranscriptionContract(t *testing.T) {
	// Byte-for-byte shape transcription-service publishes (cmd/main.go
	// publishExtractionJob + internal/model/model.go). If this stops
	// unmarshalling, the contract broke.
	raw := `{
		"schema_version": 2,
		"event_id": "b3c4c0d3-0000-4000-8000-000000000000",
		"event_type": "EXTRACTION_REQUESTED",
		"occurred_at": "2026-09-14T10:00:00.000Z",
		"data": {
			"audio_id": "04f232ee-4a3f-4883-aac6-cdfe1c133cad",
			"user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			"text": "I need to call the dentist tomorrow.",
			"language": "en",
			"speaker_segments": [
				{"speaker": 0, "start": 0.0, "end": 2.4, "confidence": 0.99, "text": "I need to call the dentist tomorrow."}
			]
		}
	}`
	var e ExtractionJobRequestedEvent
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("expected published event to validate: %v", err)
	}
}
