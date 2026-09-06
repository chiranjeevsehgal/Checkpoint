package domain

import "time"

// Outbox event types and delivery states.
const (
	EventTranscriptionRequested = "TRANSCRIPTION_REQUESTED"

	// EventAudioReadyForVAD is deprecated: pre-migration rows may still
	// carry this type. New events use EventTranscriptionRequested.
	// Kept so the dispatcher can drain old rows after deploy.
	EventAudioReadyForVAD = "AUDIO_READY_FOR_VAD"

	OutboxPending    = "PENDING"
	OutboxProcessing = "PROCESSING"
	OutboxDelivered  = "DELIVERED"
	OutboxFailed     = "FAILED"
)

// SchemaVersion versions the transcription payload contract so ingestion
// and workers can evolve independently.
const SchemaVersion = 2

// AudioReadyData is the deliverable core of the transcription job request.
// Workers fetch bytes by-reference via Bucket+ObjectKey from MinIO.
type AudioReadyData struct {
	AudioID        string `json:"audio_id"`
	UserID         string `json:"user_id"`
	Bucket         string `json:"bucket"`
	ObjectKey      string `json:"object_key"`
	ContentType    string `json:"content_type"`
	SizeBytes      int64  `json:"size_bytes"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
}

// AudioReadyPayload is the versioned envelope published toward Kafka.
type AudioReadyPayload struct {
	SchemaVersion int            `json:"schema_version"`
	EventID       string         `json:"event_id"`
	EventType     string         `json:"event_type"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Data          AudioReadyData `json:"data"`
}

// NewAudioReadyPayload builds the Kafka-bound envelope for an upload.
// checksum is the client-confirmed sha256 (may be empty when absent).
func NewAudioReadyPayload(eventID string, upload *Upload, sizeBytes int64, checksum string, now time.Time) AudioReadyPayload {
	return AudioReadyPayload{
		SchemaVersion: SchemaVersion,
		EventID:       eventID,
		EventType:     EventTranscriptionRequested,
		OccurredAt:    now.UTC(),
		Data: AudioReadyData{
			AudioID:        upload.ID,
			UserID:         upload.UserID,
			Bucket:         upload.Bucket,
			ObjectKey:      upload.ObjectKey,
			ContentType:    upload.ContentType,
			SizeBytes:      sizeBytes,
			ChecksumSHA256: checksum,
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
