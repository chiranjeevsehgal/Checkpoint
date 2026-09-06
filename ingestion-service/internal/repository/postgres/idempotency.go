package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"checkpoint/ingestion/internal/repository"
)

// Find returns the stored response for an idempotency key, or nil when
// this key has not been seen for the user.
func (p *Pool) Find(ctx context.Context, userID, key string) (*repository.IdempotencyRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var rec repository.IdempotencyRecord
	err := p.inner.QueryRow(ctx, `
		SELECT key, user_id, request_hash, response_status, response_body
		FROM idempotency_keys WHERE user_id = $1 AND key = $2`,
		userID, key).Scan(&rec.Key, &rec.UserID, &rec.RequestHash, &rec.ResponseStatus, &rec.ResponseBody)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

// Save stores a response. On concurrent duplicate keys the first writer
// wins and the second keeps the original response.
func (p *Pool) Save(ctx context.Context, rec repository.IdempotencyRecord) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := p.inner.Exec(ctx, `
		INSERT INTO idempotency_keys (key, user_id, request_hash, response_status, response_body)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (user_id, key) DO NOTHING`,
		rec.Key, rec.UserID, rec.RequestHash, rec.ResponseStatus, rec.ResponseBody)
	return err
}
