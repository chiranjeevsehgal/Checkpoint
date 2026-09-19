package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"strings"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

const (
	mcpKeySecretPrefix = "cp_mcp_"
	mcpKeySecretBytes  = 20
	mcpKeyDisplayChars = 6
)

// McpKey is the public metadata of an account's MCP access key.
type McpKey struct {
	ID         int64
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreatedMcpKey carries the one-time secret alongside the stored metadata.
type CreatedMcpKey struct {
	McpKey
	Secret string
}

// McpKeyService mints and revokes MCP access keys.
type McpKeyService struct {
	keys repository.McpKeyRepository
}

// NewMcpKeyService wires the MCP key service.
func NewMcpKeyService(keys repository.McpKeyRepository) *McpKeyService {
	return &McpKeyService{keys: keys}
}

// Create mints a key, persists only its SHA-256, and returns the secret once.
func (s *McpKeyService) Create(ctx context.Context, userID, name string) (*CreatedMcpKey, error) {
	normalized, err := domain.ValidateMcpKeyName(name)
	if err != nil {
		return nil, err
	}
	secret, keyHash, prefix, err := generateMcpKey()
	if err != nil {
		return nil, err
	}
	record := &repository.McpKey{UserID: userID, Name: normalized, Prefix: prefix}
	if err := s.keys.CreateMcpKey(ctx, record, keyHash); err != nil {
		return nil, err
	}
	return &CreatedMcpKey{McpKey: toMcpKey(record), Secret: secret}, nil
}

// List returns the user's active keys.
func (s *McpKeyService) List(ctx context.Context, userID string) ([]McpKey, error) {
	records, err := s.keys.ListMcpKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	keys := make([]McpKey, 0, len(records))
	for i := range records {
		keys = append(keys, toMcpKey(&records[i]))
	}
	return keys, nil
}

// Revoke deactivates one of the user's keys.
func (s *McpKeyService) Revoke(ctx context.Context, userID string, id int64) error {
	return s.keys.RevokeMcpKey(ctx, userID, id)
}

func toMcpKey(record *repository.McpKey) McpKey {
	return McpKey{
		ID:         record.ID,
		Name:       record.Name,
		Prefix:     record.Prefix,
		CreatedAt:  record.CreatedAt,
		LastUsedAt: record.LastUsedAt,
	}
}

// generateMcpKey returns the secret to show once, its hash to store, and a
// truncated prefix for display. Format: cp_mcp_<base32>.
func generateMcpKey() (string, []byte, string, error) {
	raw := make([]byte, mcpKeySecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	secret := mcpKeySecretPrefix + strings.ToLower(encoded)
	digest := sha256.Sum256([]byte(secret))
	prefix := secret[:len(mcpKeySecretPrefix)+mcpKeyDisplayChars]
	return secret, digest[:], prefix, nil
}
