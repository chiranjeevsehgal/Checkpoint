package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

// GetByUser returns the user's owned pendant. RLS plus the explicit
// predicate make another user's device invisible.
func (p *Pool) GetByUser(ctx context.Context, userID string) (*domain.Device, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var device *domain.Device
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		var owner pgtype.UUID
		var claimedAt pgtype.Timestamptz
		row := domain.Device{}
		err := tx.QueryRow(ctx, `
			SELECT device_id, user_id, state, claimed_at, updated_at
			FROM devices WHERE user_id = $1`, userID).
			Scan(&row.DeviceID, &owner, &row.State, &claimedAt, &row.UpdatedAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return repository.ErrDeviceNotFound
			}
			return err
		}
		row.UserID = uuidStringFromPg(owner)
		row.ClaimedAt = timeFromPg(claimedAt)
		device = &row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return device, nil
}

// IsOwnedBy reports current ownership without exposing device details.
func (p *Pool) IsOwnedBy(ctx context.Context, userID, deviceID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var owned bool
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM devices
				WHERE device_id = $1 AND user_id = $2 AND state = 'owned'
			)`, deviceID, userID).Scan(&owned)
	})
	return owned, err
}

// Claim runs the SECURITY DEFINER claim function. Every business failure
// collapses to ErrDeviceClaimFailed.
func (p *Pool) Claim(ctx context.Context, userID, deviceID string, claimHash []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT claim_device($1, $2)`, deviceID, claimHash)
		return err
	})
	if isClaimFailure(err) {
		return repository.ErrDeviceClaimFailed
	}
	return err
}

// Release returns the caller's own pendant to the unowned pool.
func (p *Pool) Release(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT release_device()`)
		return err
	})
}

func isClaimFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Message == "device_claim_failed"
}

func uuidStringFromPg(v pgtype.UUID) *string {
	if !v.Valid {
		return nil
	}
	s := uuid.UUID(v.Bytes).String()
	return &s
}
