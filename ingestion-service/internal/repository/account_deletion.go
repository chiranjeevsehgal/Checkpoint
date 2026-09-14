package repository

import "context"

// AccountDeletionRepository covers the request-side tombstone lifecycle.
type AccountDeletionRepository interface {
	// Request records a deletion tombstone. Repeats are no-ops.
	Request(ctx context.Context, userID string) error
	// IsDeleting reports whether a tombstone exists for the user.
	IsDeleting(ctx context.Context, userID string) (bool, error)
}

// AccountDeletionJob is one claimed tombstone for the deletion worker.
type AccountDeletionJob struct {
	UserID  string
	Attempt int
}

// UploadObject identifies a stored object to remove.
type UploadObject struct {
	Bucket    string
	ObjectKey string
}
