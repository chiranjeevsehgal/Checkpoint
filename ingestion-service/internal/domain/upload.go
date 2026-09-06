// Package domain holds the ingestion service's core types, state machine
// and validation rules. It depends only on the standard library so the
// rules stay testable without infrastructure.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Upload lifecycle states. VAD-owned states (processing/completed) are
// intentionally absent: this service stops at SUBMITTED.
const (
	StatusUploading = "UPLOADING"
	StatusReady     = "READY"
	StatusSubmitted = "SUBMITTED"
	StatusExpired   = "EXPIRED"
	StatusRejected  = "REJECTED"
)

// MaxUploadBytes caps the declared upload size. The pendant records OGG
// artifacts; 10 MB comfortably covers an utterance while bounding abuse.
const MaxUploadBytes = 10 * 1024 * 1024

// AllowedContentTypes lists the MIME types accepted at creation time.
// OGG-only: the recording pipeline emits OGG, and anything else is a
// client mistake. Authoritative decoding validation belongs to VAD.
var AllowedContentTypes = map[string]struct{}{
	"audio/ogg": {},
}

var (
	ErrInvalidFilename      = errors.New("filename must not be empty")
	ErrInvalidSize          = errors.New("size_bytes must be positive")
	ErrTooLarge             = errors.New("upload exceeds maximum size")
	ErrUnsupportedMediaType = errors.New("unsupported content type")
)

// Upload mirrors the uploads table row.
type Upload struct {
	ID               string
	UserID           string
	Bucket           string
	ObjectKey        string
	OriginalFilename string
	ContentType      string
	ExpectedSize     *int64
	ActualSize       *int64
	ChecksumSHA256   string
	Status           string
	UploadExpiresAt  *time.Time
	UploadedAt       *time.Time
	SubmittedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ValidateCreate checks a create-upload command before any I/O happens.
func ValidateCreate(filename, contentType string, sizeBytes int64) error {
	if strings.TrimSpace(filename) == "" {
		return ErrInvalidFilename
	}
	if sizeBytes <= 0 {
		return ErrInvalidSize
	}
	if sizeBytes > MaxUploadBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrTooLarge, sizeBytes, MaxUploadBytes)
	}
	if _, ok := AllowedContentTypes[strings.ToLower(strings.TrimSpace(contentType))]; !ok {
		return fmt.Errorf("%w: %q", ErrUnsupportedMediaType, contentType)
	}
	return nil
}

// CanTransitionToComplete reports whether /complete may move the upload
// from its current status into READY.
func CanTransitionToComplete(status string) bool {
	return status == StatusUploading
}

// IsCompletionIdempotent reports whether a repeat /complete is a safe
// no-op success instead of an error.
func IsCompletionIdempotent(status string) bool {
	return status == StatusReady || status == StatusSubmitted
}

// ObjectKeyFor builds the MinIO key for an upload. Generated keys avoid
// filename collisions and path traversal; the original filename stays in
// PostgreSQL only. The bucket name already scopes the namespace, so the
// key holds only tenant grouping and time partitioning.
func ObjectKeyFor(userID, uploadID string, now time.Time) string {
	return fmt.Sprintf("%s/%04d/%02d/%s",
		userID, now.Year(), int(now.Month()), uploadID)
}
