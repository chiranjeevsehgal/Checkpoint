// Package repository declares the persistence boundaries of the
// ingestion service. Implementations live in subpackages; the service
// layer depends only on these interfaces.
package repository

import (
	"context"
	"errors"
	"time"

	"checkpoint/ingestion/internal/domain"
)

var (
	// ErrNotFound is returned when an upload does not exist or belongs to
	// another user. Both cases map to 404 to avoid leaking ownership.
	ErrNotFound = errors.New("upload not found")
	// ErrInvalidState is returned when an operation does not apply to the
	// upload's current status.
	ErrInvalidState = errors.New("invalid upload state")
	// ErrStaleLease is returned when an outbox update targets a lease the
	// caller no longer owns: another worker reclaimed and completed the
	// event. The caller must do nothing; the rightful owner acted.
	ErrStaleLease = errors.New("stale outbox lease")
	// ErrDeviceNotFound is returned when a user has no owned pendant.
	ErrDeviceNotFound = errors.New("device not found")
	// ErrDeviceClaimFailed is returned for every claim failure, so callers
	// cannot distinguish nonexistent, foreign or quarantined devices.
	ErrDeviceClaimFailed = errors.New("device claim failed")
)

// UploadRepository covers single-row upload reads and writes.
type UploadRepository interface {
	Create(ctx context.Context, upload *domain.Upload) error
	// CreateUploadIdempotent claims the idempotency key and inserts the
	// upload in one transaction, so concurrent same-key requests collapse
	// to a single upload row.
	CreateUploadIdempotent(ctx context.Context, params IdempotentCreateParams) (*IdempotentCreateResult, error)
	GetByIDForUser(ctx context.Context, userID, uploadID string) (*domain.Upload, error)
}

// IdempotentCreateParams carries a new upload with the idempotency claim
// that must precede it. ResponseBody is the exact response to replay,
// encoded by the caller before the transaction.
type IdempotentCreateParams struct {
	Upload         *domain.Upload
	Key            string
	UserID         string
	DeviceID       string
	RequestHash    string
	ResponseStatus int
	ResponseBody   []byte
}

// IdempotentCreateResult reports whether the caller won the key claim.
// Replay is true when the key already existed; Stored then holds the
// original response for hash comparison and replay.
type IdempotentCreateResult struct {
	Replay bool
	Stored *IdempotencyRecord
}

// IdempotencyRecord stores a replayable POST /v1/uploads response so a
// client retry after a timeout cannot create a second upload.
type IdempotencyRecord struct {
	Key            string
	UserID         string
	DeviceID       string
	RequestHash    string
	ResponseStatus int
	ResponseBody   []byte
}

// IdempotencyRepository persists idempotency records keyed by
// (user_id, key), matching the table's primary key.
type IdempotencyRepository interface {
	Find(ctx context.Context, userID, key string) (*IdempotencyRecord, error)
	Save(ctx context.Context, rec IdempotencyRecord) error
}

// CompleteParams carries everything the completion transaction needs,
// including the pre-marshalled outbox payload.
type CompleteParams struct {
	UploadID   string
	UserID     string
	ActualSize int64
	Checksum   string
	EventID    string
	Payload    []byte
	Now        time.Time
}

// CompleteResult reports the outcome of the completion transaction.
type CompleteResult struct {
	Upload *domain.Upload
	// AlreadyCompleted is true when the upload was already READY or
	// SUBMITTED, making the repeat /complete a no-op success.
	AlreadyCompleted bool
}

// UploadCompletionRepository owns the atomic READY + outbox transaction.
// MarkDelivered (outbox) is the only path that flips READY to SUBMITTED,
// keeping the two in one transaction. Kept narrow on purpose instead of a
// generic transaction abstraction.
type UploadCompletionRepository interface {
	MarkReadyAndCreateEvent(ctx context.Context, params CompleteParams) (*CompleteResult, error)
}

// ClaimedEvent is one outbox row leased to a dispatcher instance.
type ClaimedEvent struct {
	ID          string
	AggregateID string
	EventType   string
	Payload     []byte
	Attempt     int
}

// OutboxRepository persists dispatcher lease and delivery outcomes.
// MarkDelivered and ScheduleRetry are fenced on the claimed attempt:
// a worker whose lease expired reports ErrStaleLease instead of
// regressing another owner's event.
type OutboxRepository interface {
	ClaimDue(ctx context.Context, instanceID string, batch int, lockFor time.Duration, now time.Time) ([]ClaimedEvent, error)
	MarkDelivered(ctx context.Context, eventID, uploadID string, attempt int, now time.Time) error
	ScheduleRetry(ctx context.Context, eventID string, attempt int, nextAttempt time.Time, errMsg string) error
	MarkFailed(ctx context.Context, eventID, errMsg string, now time.Time) error
	OutboxStats(ctx context.Context) (pending int64, oldestAge time.Duration, err error)
}

// ExpiredUpload identifies an abandoned object for best-effort removal.
type ExpiredUpload struct {
	Bucket    string
	ObjectKey string
}

// UploadCleanupRepository expires abandoned uploads.
type UploadCleanupRepository interface {
	ExpireStaleUploads(ctx context.Context, olderThan, now time.Time) ([]ExpiredUpload, error)
}
