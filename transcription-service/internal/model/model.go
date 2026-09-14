package model

import (
	"fmt"

	"github.com/google/uuid"
)

// Validate ensures tenant identifiers are canonical UUIDs before any work.
func (e TranscriptionRequestedEvent) Validate() error {
	if _, err := uuid.Parse(e.Data.AudioID); err != nil {
		return fmt.Errorf("invalid audio_id: %w", err)
	}
	if _, err := uuid.Parse(e.Data.UserID); err != nil {
		return fmt.Errorf("invalid user_id: %w", err)
	}
	return nil
}

// Envelope fields shared by every event on the bus — consumed and produced.
type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	OccurredAt    string `json:"occurred_at"`
}

// TranscriptionRequestedEvent is what Ingestion publishes to topic once an audio file has landed in MinIO.
type TranscriptionRequestedEvent struct {
	Envelope
	Data TranscriptionRequestedData `json:"data"`
}

type TranscriptionRequestedData struct {
	AudioID        string `json:"audio_id"`
	UserID         string `json:"user_id"`
	Bucket         string `json:"bucket"`
	ObjectKey      string `json:"object_key"`
	ContentType    string `json:"content_type"`
	SizeBytes      int64  `json:"size_bytes"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

// SpeakerSegment mirrors a single utterance from the provider.
type SpeakerSegment struct {
	Speaker    int     `json:"speaker"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Confidence float64 `json:"confidence"`
	Text       string  `json:"text"`
}

// TranscriptResult is the internal, normalized shape every provider implementation returns — provider-agnostic, used to build bothv downstream events and the Postgres row.
type TranscriptResult struct {
	AudioID         string           `json:"audio_id"`
	UserID          string           `json:"user_id"`
	Text            string           `json:"text"`
	Language        string           `json:"language"`
	DurationSeconds float64          `json:"duration_seconds"`
	SpeakerSegments []SpeakerSegment `json:"speaker_segments"`
	Provider        string           `json:"provider"`
	RequestID       string           `json:"request_id"`
}

type EmbeddingJobRequestedEvent struct {
	Envelope
	Data EmbeddingJobData `json:"data"`
}

type EmbeddingJobData struct {
	AudioID  string `json:"audio_id"`
	UserID   string `json:"user_id"`
	Text     string `json:"text"`
	Language string `json:"language"`
}

type ExtractionJobRequestedEvent struct {
	Envelope
	Data ExtractionJobData `json:"data"`
}

type ExtractionJobData struct {
	AudioID         string           `json:"audio_id"`
	UserID          string           `json:"user_id"`
	Text            string           `json:"text"`
	Language        string           `json:"language"`
	SpeakerSegments []SpeakerSegment `json:"speaker_segments"`
}