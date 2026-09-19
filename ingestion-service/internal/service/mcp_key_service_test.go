package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

type fakeMcpKeys struct {
	created     *repository.McpKey
	createdHash []byte
	rows        []repository.McpKey
	revokedID   int64
	err         error
}

func (f *fakeMcpKeys) CreateMcpKey(_ context.Context, key *repository.McpKey, keyHash []byte) error {
	if f.err != nil {
		return f.err
	}
	key.ID = 7
	key.CreatedAt = time.Now()
	f.created = key
	f.createdHash = keyHash
	return nil
}

func (f *fakeMcpKeys) ListMcpKeys(_ context.Context, _ string) ([]repository.McpKey, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeMcpKeys) RevokeMcpKey(_ context.Context, _ string, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.revokedID = id
	return nil
}

func TestMcpKeyCreatePersistsOnlyHash(t *testing.T) {
	repo := &fakeMcpKeys{}
	svc := NewMcpKeyService(repo)

	created, err := svc.Create(context.Background(), "user-1", " laptop ")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(created.Secret, mcpKeySecretPrefix) {
		t.Fatalf("secret %q missing prefix", created.Secret)
	}
	if created.Name != "laptop" {
		t.Fatalf("name: got %q, want laptop", created.Name)
	}
	if created.ID != 7 {
		t.Fatalf("id: got %d, want 7", created.ID)
	}
	if created.Prefix != created.Secret[:len(mcpKeySecretPrefix)+mcpKeyDisplayChars] {
		t.Fatalf("prefix %q does not derive from secret", created.Prefix)
	}
	sum := sha256.Sum256([]byte(created.Secret))
	if !bytes.Equal(repo.createdHash, sum[:]) {
		t.Fatal("persisted hash does not match the secret")
	}
	if repo.created.Prefix != created.Prefix {
		t.Fatal("stored prefix does not match the returned metadata")
	}
}

func TestMcpKeyCreateRejectsEmptyName(t *testing.T) {
	repo := &fakeMcpKeys{}
	svc := NewMcpKeyService(repo)

	if _, err := svc.Create(context.Background(), "user-1", "   "); !errors.Is(err, domain.ErrInvalidKeyName) {
		t.Fatalf("want ErrInvalidKeyName, got %v", err)
	}
	if repo.created != nil {
		t.Fatal("invalid name must not persist a key")
	}
}

func TestMcpKeyListMapsRows(t *testing.T) {
	repo := &fakeMcpKeys{rows: []repository.McpKey{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}}
	svc := NewMcpKeyService(repo)

	keys, err := svc.List(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 || keys[1].Name != "b" {
		t.Fatalf("unexpected keys: %+v", keys)
	}
}

func TestMcpKeyRevokeDelegates(t *testing.T) {
	repo := &fakeMcpKeys{}
	svc := NewMcpKeyService(repo)

	if err := svc.Revoke(context.Background(), "user-1", 42); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if repo.revokedID != 42 {
		t.Fatalf("revoked id: got %d, want 42", repo.revokedID)
	}
}
