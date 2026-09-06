// Package cleanup expires abandoned uploads. A small ticker marks stale
// UPLOADING rows EXPIRED and removes their orphaned objects on a best
// effort basis; storage failures are logged, never fatal.
package cleanup

import (
	"context"
	"log/slog"
	"time"

	"checkpoint/ingestion/internal/repository"
)

// Defaults: uploads abandoned for a day expire; the sweep runs every
// 30 minutes. Both are configurable through the constructor.
const (
	DefaultUploadExpiry = 24 * time.Hour
	DefaultInterval     = 30 * time.Minute
)

// Deleter removes one stored object. *minioimpl.Storage satisfies it.
type Deleter interface {
	DeleteObject(ctx context.Context, bucket, objectKey string) error
}

// Cleaner runs the expiry sweep on an interval.
type Cleaner struct {
	store   repository.UploadCleanupRepository
	objects Deleter
	expiry  time.Duration
	every   time.Duration
	now     func() time.Time
	log     *slog.Logger
}

// NewCleaner wires a cleaner with explicit timings.
func NewCleaner(
	store repository.UploadCleanupRepository,
	objects Deleter,
	expiry, every time.Duration,
	log *slog.Logger,
) *Cleaner {
	if log == nil {
		log = slog.Default()
	}
	return &Cleaner{
		store: store, objects: objects,
		expiry: expiry, every: every,
		now: func() time.Time { return time.Now().UTC() },
		log: log,
	}
}

// Run sweeps on every interval until ctx is done.
func (c *Cleaner) Run(ctx context.Context) {
	ticker := time.NewTicker(c.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.log.Info("upload cleaner stopping")
			return
		case <-ticker.C:
			expired, deleted, err := c.Sweep(ctx)
			if err != nil {
				c.log.Error("expiry sweep failed", "error", err)
				continue
			}
			if expired > 0 {
				c.log.Info("expiry sweep done", "expired", expired, "objects_deleted", deleted)
			}
		}
	}
}

// Sweep expires stale uploads once and removes their objects. A storage
// failure for one object never blocks the rest.
func (c *Cleaner) Sweep(ctx context.Context) (expired, deleted int, err error) {
	now := c.now()
	stale, err := c.store.ExpireStaleUploads(ctx, now.Add(-c.expiry), now)
	if err != nil {
		return 0, 0, err
	}
	for _, s := range stale {
		if err := c.objects.DeleteObject(ctx, s.Bucket, s.ObjectKey); err != nil {
			c.log.Warn("orphan object delete failed",
				"bucket", s.Bucket, "key", s.ObjectKey, "error", err)
			continue
		}
		deleted++
	}
	return len(stale), deleted, nil
}
