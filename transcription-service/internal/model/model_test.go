package model

import (
	"encoding/json"
	"testing"
)

func TestValidateRejectsMalformedIDs(t *testing.T) {
	valid := TranscriptionRequestedEvent{
		Data: TranscriptionRequestedData{
			AudioID: "11111111-1111-1111-1111-111111111111",
			UserID:  "22222222-2222-2222-2222-222222222222",
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid ids must pass: %v", err)
	}

	cases := map[string]TranscriptionRequestedEvent{
		"bad audio id": {Data: TranscriptionRequestedData{AudioID: "not-a-uuid", UserID: valid.Data.UserID}},
		"bad user id":  {Data: TranscriptionRequestedData{AudioID: valid.Data.AudioID, UserID: ""}},
	}
	for name, event := range cases {
		t.Run(name, func(t *testing.T) {
			if err := event.Validate(); err == nil {
				t.Fatal("malformed id must be rejected")
			}
		})
	}
}

func TestRecordedAtRoundTrip(t *testing.T) {
	raw := `{
		"schema_version": 3,
		"event_id": "e1",
		"event_type": "TRANSCRIPTION_REQUESTED",
		"occurred_at": "2026-09-14T10:00:00Z",
		"data": {
			"audio_id": "11111111-1111-1111-1111-111111111111",
			"user_id": "22222222-2222-2222-2222-222222222222",
			"bucket": "audio",
			"object_key": "k",
			"recorded_at": "2026-09-13T10:15:00Z"
		}
	}`
	var e TranscriptionRequestedEvent
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if e.Data.RecordedAt != "2026-09-13T10:15:00Z" {
		t.Fatalf("recorded_at = %q, want 2026-09-13T10:15:00Z", e.Data.RecordedAt)
	}

	out, err := json.Marshal(e.Data)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var back TranscriptionRequestedData
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if back.RecordedAt != e.Data.RecordedAt {
		t.Fatalf("round-trip lost recorded_at: %q", back.RecordedAt)
	}
}

func TestRecordedAtOmittedWhenAbsent(t *testing.T) {
	out, err := json.Marshal(TranscriptionRequestedData{
		AudioID: "11111111-1111-1111-1111-111111111111",
		UserID:  "22222222-2222-2222-2222-222222222222",
	})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if _, ok := data["recorded_at"]; ok {
		t.Fatal("absent recorded_at must be omitted")
	}
}
