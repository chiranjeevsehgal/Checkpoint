// Package deletion runs the resumable account-deletion worker inside the
// ingestion process.
package deletion

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"checkpoint/ingestion/internal/auth"
	"checkpoint/ingestion/internal/repository"
)

const (
	defaultBatch    = 5
	defaultInterval = 15 * time.Second
)

// PurgeRepository is the worker-side persistence boundary.
type PurgeRepository interface {
	ClaimDeletions(ctx context.Context, batch int, now time.Time) ([]repository.AccountDeletionJob, error)
	ListUploadObjects(ctx context.Context, userID string) ([]repository.UploadObject, error)
	GetNotificationChannel(ctx context.Context, userID string) (*repository.NotificationChannel, error)
	PurgeIngestion(ctx context.Context, userID string) error
	QuarantineDevice(ctx context.Context, userID string) error
	PurgeDownstream(ctx context.Context, userID string) error
	CompleteDeletion(ctx context.Context, userID string, now time.Time) error
	RetryDeletion(ctx context.Context, userID string, next time.Time, errMsg string, now time.Time) error
}

// ObjectDeleter removes a stored object.
type ObjectDeleter interface {
	DeleteObject(ctx context.Context, bucket, objectKey string) error
}

// NtfyUserDeleter removes a provisioned ntfy user during account deletion.
type NtfyUserDeleter interface {
	DeleteUser(ctx context.Context, username string) error
}

// Worker drains due tombstones and purges user data.
type Worker struct {
	deletions  PurgeRepository
	objects    ObjectDeleter
	identities auth.IdentityDeleter
	ntfyUsers  NtfyUserDeleter
	logger     *slog.Logger
	interval   time.Duration
	now        func() time.Time
}

// NewWorker wires the deletion worker. identities and ntfyUsers may be nil when
// their backing services are not configured.
func NewWorker(deletions PurgeRepository, objects ObjectDeleter, identities auth.IdentityDeleter, ntfyUsers NtfyUserDeleter, logger *slog.Logger) *Worker {
	return &Worker{
		deletions:  deletions,
		objects:    objects,
		identities: identities,
		ntfyUsers:  ntfyUsers,
		logger:     logger,
		interval:   defaultInterval,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// Run sweeps until the context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Sweep(ctx)
		}
	}
}

// Sweep claims and processes one batch of deletions.
func (w *Worker) Sweep(ctx context.Context) {
	jobs, err := w.deletions.ClaimDeletions(ctx, defaultBatch, w.now())
	if err != nil {
		w.logger.Error("account_deletion_claim_failed", "error", err)
		return
	}
	for _, job := range jobs {
		w.process(ctx, job)
	}
}

func (w *Worker) process(ctx context.Context, job repository.AccountDeletionJob) {
	w.logger.Info("account_deletion_started", "user_id", job.UserID, "attempt", job.Attempt)

	if err := w.purgeObjects(ctx, job.UserID); err != nil {
		w.retry(ctx, job, err)
		return
	}
	w.purgeNtfyUser(ctx, job.UserID)
	if err := w.deletions.PurgeIngestion(ctx, job.UserID); err != nil {
		w.retry(ctx, job, err)
		return
	}
	if err := w.deletions.QuarantineDevice(ctx, job.UserID); err != nil {
		w.retry(ctx, job, err)
		return
	}
	if err := w.deletions.PurgeDownstream(ctx, job.UserID); err != nil {
		w.retry(ctx, job, err)
		return
	}
	if w.identities != nil {
		if err := w.identities.DeleteIdentity(ctx, job.UserID); err != nil {
			w.retry(ctx, job, err)
			return
		}
	}
	if err := w.deletions.CompleteDeletion(ctx, job.UserID, w.now()); err != nil {
		w.retry(ctx, job, err)
		return
	}
	w.logger.Info("account_deletion_completed", "user_id", job.UserID)
}

// purgeNtfyUser removes the user's provisioned ntfy account. Best-effort: a
// failure leaves an orphan that a later enable or deletion attempt can clean up
// and must never block the local purge.
func (w *Worker) purgeNtfyUser(ctx context.Context, userID string) {
	if w.ntfyUsers == nil {
		return
	}
	channel, err := w.deletions.GetNotificationChannel(ctx, userID)
	if err != nil {
		w.logger.Warn("account_deletion_notification_lookup_failed", "user_id", userID, "error", err)
		return
	}
	if channel == nil || channel.Username == "" {
		return
	}
	if err := w.ntfyUsers.DeleteUser(ctx, channel.Username); err != nil {
		w.logger.Warn("account_deletion_ntfy_cleanup_failed", "user_id", userID, "error", err)
	}
}

func (w *Worker) purgeObjects(ctx context.Context, userID string) error {
	objects, err := w.deletions.ListUploadObjects(ctx, userID)
	if err != nil {
		return fmt.Errorf("list upload objects: %w", err)
	}
	for _, object := range objects {
		if err := w.objects.DeleteObject(ctx, object.Bucket, object.ObjectKey); err != nil {
			return fmt.Errorf("delete object %s/%s: %w", object.Bucket, object.ObjectKey, err)
		}
	}
	return nil
}

func (w *Worker) retry(ctx context.Context, job repository.AccountDeletionJob, cause error) {
	now := w.now()
	next := now.Add(retryDelay(job.Attempt))
	w.logger.Error("account_deletion_retry",
		"user_id", job.UserID, "attempt", job.Attempt, "error", cause, "next_attempt_at", next)
	if err := w.deletions.RetryDeletion(ctx, job.UserID, next, cause.Error(), now); err != nil {
		w.logger.Error("account_deletion_retry_failed", "user_id", job.UserID, "error", err)
	}
}

func retryDelay(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 5 * time.Second
	case attempt == 2:
		return 30 * time.Second
	case attempt == 3:
		return 2 * time.Minute
	default:
		return 10 * time.Minute
	}
}
