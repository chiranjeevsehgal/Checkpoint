package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"checkpoint/ingestion/internal/repository"
)

// CreateMcpKey inserts a hashed key under the caller's RLS context.
func (p *Pool) CreateMcpKey(ctx context.Context, key *repository.McpKey, keyHash []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, key.UserID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO mcp_access_keys (user_id, name, key_prefix, key_hash)
			VALUES ($1, $2, $3, $4)
			RETURNING id, created_at`, key.UserID, key.Name, key.Prefix, keyHash).
			Scan(&key.ID, &key.CreatedAt)
	})
}

// ListMcpKeys returns the user's unrevoked keys, newest first.
func (p *Pool) ListMcpKeys(ctx context.Context, userID string) ([]repository.McpKey, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	keys := []repository.McpKey{}
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, name, key_prefix, created_at, last_used_at
			FROM mcp_access_keys
			WHERE user_id = $1 AND revoked_at IS NULL
			ORDER BY created_at DESC`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var key repository.McpKey
			var lastUsed pgtype.Timestamptz
			if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &key.CreatedAt, &lastUsed); err != nil {
				return err
			}
			key.UserID = userID
			key.LastUsedAt = timeFromPg(lastUsed)
			keys = append(keys, key)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// RevokeMcpKey marks the key revoked. A repeated revoke is a no-op.
func (p *Pool) RevokeMcpKey(ctx context.Context, userID string, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE mcp_access_keys SET revoked_at = NOW()
			WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
		return err
	})
}
