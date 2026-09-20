package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Envelope fields shared by every event on the bus — consumed and produced.
type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	OccurredAt    string `json:"occurred_at"`
}

const (
	SchemaVersion           = 2
	EventTypeRetryRequested = "RETRY_REQUESTED"
	EventTypeRetryFailed    = "RETRY_FAILED"
)

// RetryRequestedEvent is what a failing service publishes to retry.jobs.v1
// to hand a failed message over for delayed re-delivery. The original event
// rides along unmodified inside data.original_event, exactly like the DLQ
// convention, so re-delivery republishes byte-identical payloads.
type RetryRequestedEvent struct {
	Envelope
	Data RetryRequestedData `json:"data"`
}

type RetryRequestedData struct {
	SourceService string          `json:"source_service"`
	SourceTopic   string          `json:"source_topic"`
	// Stage is where in the producer's pipeline the failure happened
	// (e.g. "download_audio", "provider_call", "persist"), so operators
	// can see not just that a retry failed but at which point.
	Stage         string          `json:"stage"`
	ErrorCode     string          `json:"error_code"`
	ErrorMessage  string          `json:"error_message"`
	OriginalEvent json.RawMessage `json:"original_event"`
}

// RetryFailedEvent is the DLQ envelope for poison messages on the retry
// topic. Same shape the other services write to their own .dlq topics.
type RetryFailedEvent struct {
	Envelope
	Data RetryFailedData `json:"data"`
}

type RetryFailedData struct {
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

// ParsedOriginal is the retry identity extracted from the embedded original
// event: its envelope event_id keys the retry_jobs row (together with the
// source topic), and data.user_id — present on every job event today —
// drives tombstone gating and the account-deletion purge.
type ParsedOriginal struct {
	EventID string
	// UserID is empty when the original event carries no user_id.
	UserID string
}

// Validate enforces the handoff contract and extracts the original event's
// identity. Anything failing here is poison and belongs in the DLQ, not in
// the retry queue.
func (e *RetryRequestedEvent) Validate() (ParsedOriginal, error) {
	if e.SchemaVersion != SchemaVersion {
		return ParsedOriginal{}, &InvalidEvent{fmt.Sprintf("unsupported schema_version: %d", e.SchemaVersion)}
	}
	if e.EventType != EventTypeRetryRequested {
		return ParsedOriginal{}, &InvalidEvent{fmt.Sprintf("unexpected event_type: %q", e.EventType)}
	}
	if strings.TrimSpace(e.Data.SourceService) == "" {
		return ParsedOriginal{}, &InvalidEvent{"missing source_service"}
	}
	if strings.TrimSpace(e.Data.SourceTopic) == "" {
		return ParsedOriginal{}, &InvalidEvent{"missing source_topic"}
	}
	if len(e.Data.OriginalEvent) == 0 {
		return ParsedOriginal{}, &InvalidEvent{"missing original_event"}
	}

	var orig struct {
		Envelope
		Data struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.Data.OriginalEvent, &orig); err != nil {
		return ParsedOriginal{}, &InvalidEvent{"original_event is not valid json: " + err.Error()}
	}
	if _, err := uuid.Parse(orig.EventID); err != nil {
		return ParsedOriginal{}, &InvalidEvent{"original_event.event_id must be a UUID"}
	}
	userID := ""
	if strings.TrimSpace(orig.Data.UserID) != "" {
		if _, err := uuid.Parse(orig.Data.UserID); err != nil {
			return ParsedOriginal{}, &InvalidEvent{"original_event.data.user_id must be a UUID when present"}
		}
		userID = orig.Data.UserID
	}
	return ParsedOriginal{EventID: orig.EventID, UserID: userID}, nil
}

// AttemptEntry is one failure report in a retry job's audit log: which
// stage failed, with what error, and when the handoff arrived.
type AttemptEntry struct {
	AttemptedAt  string `json:"attempted_at"`
	Stage        string `json:"stage"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

// FailureRecord is everything the consumer distilled from one handoff event,
// ready to be upserted into retry_jobs.
type FailureRecord struct {
	OriginalEventID string
	SourceService   string
	SourceTopic     string
	// MessageKey is the Kafka key of the original message so re-delivery
	// lands on the same source-topic partition; empty when there was none.
	MessageKey      string
	// UserID is empty when the original event carries no user_id.
	UserID          string
	OriginalPayload []byte
	Entry           AttemptEntry
}

// RetryJob is a claimed row handed to the dispatcher for re-delivery.
type RetryJob struct {
	ID              string
	OriginalEventID string
	SourceService   string
	SourceTopic     string
	MessageKey      string
	UserID          string
	OriginalPayload []byte
	// Attempts counts completed dispatches before this claim.
	Attempts int
}

// RecordOutcome says what RecordFailure did to the retry job, so the
// consumer can log an accurate one-line story per handoff.
type RecordOutcome int

const (
	// OutcomeScheduled: first failure seen — row created, first retry
	// scheduled for now + delay.
	OutcomeScheduled RecordOutcome = iota
	// OutcomeRearmed: a dispatched retry failed again and attempts remain —
	// the next retry is scheduled for now + delay.
	OutcomeRearmed
	// OutcomeFailed: a dispatched retry failed again and attempts are
	// exhausted — terminal, no further re-delivery.
	OutcomeFailed
	// OutcomeDuplicate: a handoff arrived while a retry was still pending
	// or in flight (duplicate publish or pre-dispatch double failure) —
	// the failure is logged but the existing schedule stands.
	OutcomeDuplicate
	// OutcomeTerminal: a handoff arrived for an already-failed job —
	// ignored, the row is immutable once terminal.
	OutcomeTerminal
)

func (o RecordOutcome) String() string {
	switch o {
	case OutcomeScheduled:
		return "scheduled"
	case OutcomeRearmed:
		return "rearmed"
	case OutcomeFailed:
		return "failed"
	case OutcomeDuplicate:
		return "duplicate"
	case OutcomeTerminal:
		return "terminal"
	default:
		return "unknown"
	}
}
