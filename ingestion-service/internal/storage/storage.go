// Package storage declares the object-storage boundary. The rest of the
// application depends only on ObjectStorage and never on the MinIO SDK.
package storage

import (
	"context"
	"errors"
	"time"
)

// ErrObjectNotFound is returned when the expected object is absent, e.g.
// the client called /complete without uploading anything.
var ErrObjectNotFound = errors.New("object not found")

// ObjectInfo describes a stored object without fetching its bytes.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// ObjectStorage is the sole seam between the service and MinIO.
type ObjectStorage interface {
	CreateUploadURL(
		ctx context.Context,
		bucket string,
		objectKey string,
		contentType string,
		expiry time.Duration,
	) (string, error)

	StatObject(
		ctx context.Context,
		bucket string,
		objectKey string,
	) (ObjectInfo, error)

	DeleteObject(
		ctx context.Context,
		bucket string,
		objectKey string,
	) error
}
