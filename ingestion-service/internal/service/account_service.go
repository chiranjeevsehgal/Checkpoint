package service

import (
	"context"

	"checkpoint/ingestion/internal/repository"
)

// AccountService owns the request-side account lifecycle.
type AccountService struct {
	deletions repository.AccountDeletionRepository
}

// NewAccountService wires the account service.
func NewAccountService(deletions repository.AccountDeletionRepository) *AccountService {
	return &AccountService{deletions: deletions}
}

// RequestDeletion records a durable deletion tombstone.
func (s *AccountService) RequestDeletion(ctx context.Context, userID string) error {
	return s.deletions.Request(ctx, userID)
}
