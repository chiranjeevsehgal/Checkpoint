// Package identitycleanup removes identities that never verified their email.
// Kratos creates the identity at registration, so abandoned or mistyped
// signups would otherwise persist forever.
package identitycleanup

import (
	"context"
	"log/slog"
	"time"

	"checkpoint/ingestion/internal/auth"
)

const (
	// DefaultTTL is how old an unverified identity must be before deletion.
	DefaultTTL = time.Hour
	// DefaultInterval is how often the sweep runs.
	DefaultInterval = 15 * time.Minute

	defaultPageSize      = 250
	maxDeletionsPerSweep = 500
)

// IdentityLister pages through identities in the identity provider.
type IdentityLister interface {
	ListIdentities(ctx context.Context, pageSize int, pageToken string) ([]auth.Identity, string, error)
}

// IdentityDeleter removes one identity.
type IdentityDeleter interface {
	DeleteIdentity(ctx context.Context, identityID string) error
}

// Worker deletes identities that never completed email verification.
type Worker struct {
	lister   IdentityLister
	deleter  IdentityDeleter
	ttl      time.Duration
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

// NewWorker wires a cleanup worker with explicit timings.
func NewWorker(lister IdentityLister, deleter IdentityDeleter, ttl, interval time.Duration, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		lister:   lister,
		deleter:  deleter,
		ttl:      ttl,
		interval: interval,
		now:      func() time.Time { return time.Now().UTC() },
		logger:   logger,
	}
}

// Run sweeps once immediately, then on every interval until ctx is done.
func (w *Worker) Run(ctx context.Context) {
	w.sweepAndLog(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("identity_cleanup_stopping")
			return
		case <-ticker.C:
			w.sweepAndLog(ctx)
		}
	}
}

func (w *Worker) sweepAndLog(ctx context.Context) {
	deleted, err := w.Sweep(context.WithoutCancel(ctx))
	if err != nil {
		w.logger.Error("identity_cleanup_sweep_failed", "error", err)
		return
	}
	if deleted > 0 {
		w.logger.Info("identity_cleanup_sweep_done", "deleted", deleted)
	}
}

// Sweep collects stale unverified identities, then deletes them. Collecting
// first avoids cursoring over rows that are being removed.
func (w *Worker) Sweep(ctx context.Context) (int, error) {
	candidates, err := w.stale(ctx, w.now().Add(-w.ttl))
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, identity := range candidates {
		if err := w.deleter.DeleteIdentity(ctx, identity.ID); err != nil {
			w.logger.Error("unverified_identity_delete_failed", "identity_id", identity.ID, "error", err)
			continue
		}
		deleted++
		w.logger.Info("unverified_identity_deleted", "identity_id", identity.ID, "created_at", identity.CreatedAt)
	}
	return deleted, nil
}

func (w *Worker) stale(ctx context.Context, cutoff time.Time) ([]auth.Identity, error) {
	var candidates []auth.Identity
	pageToken := ""
	for {
		page, next, err := w.lister.ListIdentities(ctx, defaultPageSize, pageToken)
		if err != nil {
			return nil, err
		}
		for _, identity := range page {
			if !auth.HasVerifiedEmail(identity.VerifiableAddresses) && identity.CreatedAt.Before(cutoff) {
				candidates = append(candidates, identity)
			}
		}
		if next == "" || len(candidates) >= maxDeletionsPerSweep {
			break
		}
		pageToken = next
	}
	if len(candidates) > maxDeletionsPerSweep {
		candidates = candidates[:maxDeletionsPerSweep]
	}
	return candidates, nil
}
