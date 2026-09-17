package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Envelope fields shared by every event on the bus — consumed and produced.
type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	OccurredAt    string `json:"occurred_at"`
}

const (
	SchemaVersion                = 2
	EventTypeExtractionRequested = "EXTRACTION_REQUESTED"
	EventTypeExtractionFailed    = "EXTRACTION_FAILED"
)

// ExtractionJobRequestedEvent is what transcription publishes to
// extraction.jobs.v1 once a transcript is ready.
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

// SpeakerSegment mirrors a single utterance from the transcription provider.
type SpeakerSegment struct {
	Speaker    int     `json:"speaker"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Confidence float64 `json:"confidence"`
	Text       string `json:"text"`
}

// ExtractionFailedEvent is the DLQ envelope for poison messages. Same shape
// the embedding service writes to its own .dlq topic.
type ExtractionFailedEvent struct {
	Envelope
	Data ExtractionFailedData `json:"data"`
}

type ExtractionFailedData struct {
	SourceTopic   string          `json:"source_topic"`
	ErrorCode     string          `json:"error_code"`
	ErrorMessage  string          `json:"error_message"`
	OriginalEvent json.RawMessage `json:"original_event"`
}

// InvalidEvent marks a poison message: bad JSON, wrong envelope, or unusable
// data. It never succeeds on redelivery, so the caller routes it to the DLQ
// instead of retrying.
type InvalidEvent struct {
	Reason string
}

func (i *InvalidEvent) Error() string { return i.Reason }

// Validate enforces the envelope contract. Anything failing here is poison
// and belongs in the DLQ, not in a retry loop.
func (e *ExtractionJobRequestedEvent) Validate() error {
	if e.SchemaVersion != SchemaVersion {
		return &InvalidEvent{fmt.Sprintf("unsupported schema_version: %d", e.SchemaVersion)}
	}
	if e.EventType != EventTypeExtractionRequested {
		return &InvalidEvent{fmt.Sprintf("unexpected event_type: %q", e.EventType)}
	}
	if strings.TrimSpace(e.Data.AudioID) == "" {
		return &InvalidEvent{"missing audio_id"}
	}
	if strings.TrimSpace(e.Data.UserID) == "" {
		return &InvalidEvent{"missing user_id"}
	}
	if strings.TrimSpace(e.Data.Text) == "" {
		return &InvalidEvent{"missing text"}
	}
	return nil
}

// Job is one pending extraction unit persisted in extraction_jobs — the
// durable batch queue. Consumer goroutines only insert these; the batcher
// claims them N at a time for LLM calls.
type Job struct {
	ID             int64
	UserID         string
	AudioID        string
	ExtractionType string
	Text           string
	Language       string
	// Attempts is the value after claiming (claim increments before use).
	Attempts int
}

// Registered extraction types; extraction_jobs.extraction_type and
// model.Result.ExtractionType both use these values.
const (
	TypeTodo     = "todo"
	TypeReminder = "reminder"
	TypeInsight  = "insight"
)

// Result is what an extractor produced for one claimed job. Every claimed
// job always yields a Result — empty output is a valid outcome ("no action
// items") and still replaces any stale rows for that audio. Only the field
type Result struct {
	JobID          int64
	UserID         string
	AudioID        string
	ExtractionType string
	Todos          []string
	Reminders      []Reminder
	Insights       []Insight
}

// Reminder is a time-bound commitment: the LLM resolves the stated date
// and/or time against the current time sent in the prompt. A nil RemindAt
// means the statement was clearly time-bound but no concrete time could
// be resolved.
type Reminder struct {
	Text     string
	RemindAt *time.Time
}

// Insight is a reflection, realization, idea, or conclusion worth remembering — deliberately not an action item.
type Insight struct {
	Text string
}
