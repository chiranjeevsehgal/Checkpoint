package repository

import (
	"context"
	"time"
)

// McpKey is an account's MCP access-key metadata. The secret itself is never
// persisted or returned here; Prefix is a truncated display value.
type McpKey struct {
	ID         int64
	UserID     string
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// McpKeyRepository persists per-user MCP access keys. Every method runs under
// the RLS user context, so a caller can never read or revoke another user's key.
type McpKeyRepository interface {
	// CreateMcpKey inserts a key and fills ID and CreatedAt.
	CreateMcpKey(ctx context.Context, key *McpKey, keyHash []byte) error
	// ListMcpKeys returns the user's active keys, newest first.
	ListMcpKeys(ctx context.Context, userID string) ([]McpKey, error)
	// RevokeMcpKey marks one of the user's keys revoked. Unknown ids are a no-op.
	RevokeMcpKey(ctx context.Context, userID string, id int64) error
}
