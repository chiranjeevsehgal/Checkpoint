package domain

import "time"

// Outbox event types and delivery states.
const (
	EventAudioReadyForVAD = "AUDIO_READY_FOR_VAD"

	OutboxPending    = "PENDING"
	OutboxProcessing = "PROCESSING"
	OutboxDelivered  = "DELIVERED"
	OutboxFailed     = "FAILED"
)

// SchemaVersion versions the VAD payload contract so both services can
// evolve independently.
const SchemaVersion = 1

// AudioReadyData is the deliverable core of the VAD job request.
type AudioReadyData struct {
	AudioID     string `json:"audio_id"`
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

// AudioReadyPayload is the versioned envelope sent toward VAD.
type AudioReadyPayload struct {
	SchemaVersion int            `json:"schema_version"`
	EventID       string         `json:"event_id"`
	EventType     string         `json:"event_type"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Data          AudioReadyData `json:"data"`
}

// NewAudioReadyPayload builds the VAD-bound envelope for an upload.
func NewAudioReadyPayload(eventID string, upload *Upload, sizeBytes int64, now time.Time) AudioReadyPayload {
	return AudioReadyPayload{
		SchemaVersion: SchemaVersion,
		EventID:       eventID,
		EventType:     EventAudioReadyForVAD,
		OccurredAt:    now.UTC(),
		Data: AudioReadyData{
			AudioID:     upload.ID,
			Bucket:      upload.Bucket,
			ObjectKey:   upload.ObjectKey,
			ContentType: upload.ContentType,
			SizeBytes:   sizeBytes,
		},
	}
}

// NextRetryDelay returns how long the dispatcher waits before attempt n.
// Attempt counting starts at 1 (first failure). Delay is capped at 5
// minutes and retries continue indefinitely; FAILED is reserved for
// operational intervention, not transient outages.
func NextRetryDelay(failedAttempt int) time.Duration {
	switch {
	case failedAttempt <= 1:
		return 5 * time.Second
	case failedAttempt == 2:
		return 15 * time.Second
	case failedAttempt == 3:
		return time.Minute
	default:
		return 5 * time.Minute
	}
}
