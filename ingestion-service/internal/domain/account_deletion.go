package domain

import "time"

// Account deletion tombstone states.
const (
	DeletionPending    = "PENDING"
	DeletionProcessing = "PROCESSING"
	DeletionComplete   = "COMPLETE"
)

// AccountDeletion is the durable tombstone backing account removal.
type AccountDeletion struct {
	UserID       string
	Status       string
	AttemptCount int
	NextAttempt  time.Time
	LastError    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CompletedAt  *time.Time
}
