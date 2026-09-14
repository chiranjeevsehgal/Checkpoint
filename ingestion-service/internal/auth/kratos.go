// Package auth validates opaque session tokens against Ory Kratos and
// derives the caller's permanent identity server-side.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrInvalidSession means the credential is missing, expired or revoked.
	ErrInvalidSession = errors.New("invalid session")
	// ErrVerificationRequired means the identity is valid but its email is
	// not yet verified.
	ErrVerificationRequired = errors.New("verification required")
	// ErrProviderUnavailable means Kratos could not be reached or returned
	// an unexpected response. It must never be treated as logout.
	ErrProviderUnavailable = errors.New("auth provider unavailable")
)

// Principal is the authenticated caller. There is deliberately no email:
// authorization only needs the immutable identity and its session age.
type Principal struct {
	UserID          string
	AuthenticatedAt time.Time
}

// Authenticator turns a bearer session token into a Principal.
type Authenticator interface {
	Authenticate(ctx context.Context, sessionToken string) (Principal, error)
}

// KratosAuthenticator validates native Kratos session tokens.
type KratosAuthenticator struct {
	publicURL string
	client    *http.Client
}

// NewKratosAuthenticator builds a client for the Kratos public API.
func NewKratosAuthenticator(publicURL string, timeout time.Duration) *KratosAuthenticator {
	return &KratosAuthenticator{
		publicURL: strings.TrimRight(publicURL, "/"),
		client:    &http.Client{Timeout: timeout},
	}
}

type whoamiSession struct {
	Active          bool   `json:"active"`
	AuthenticatedAt string `json:"authenticated_at"`
	Identity        struct {
		ID                  string `json:"id"`
		VerifiableAddresses []struct {
			Via      string `json:"via"`
			Verified bool   `json:"verified"`
			Status   string `json:"status"`
		} `json:"verifiable_addresses"`
	} `json:"identity"`
}

// Authenticate calls GET /sessions/whoami and maps the result to a Principal
// or a classified error.
func (a *KratosAuthenticator) Authenticate(ctx context.Context, sessionToken string) (Principal, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.publicURL+"/sessions/whoami", nil)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	req.Header.Set("X-Session-Token", sessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Principal{}, ErrInvalidSession
	}
	if resp.StatusCode != http.StatusOK {
		return Principal{}, fmt.Errorf("%w: kratos status %d", ErrProviderUnavailable, resp.StatusCode)
	}

	var session whoamiSession
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&session); err != nil {
		return Principal{}, fmt.Errorf("%w: decode session: %v", ErrProviderUnavailable, err)
	}
	if !session.Active {
		return Principal{}, ErrInvalidSession
	}

	userID, err := uuid.Parse(session.Identity.ID)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: identity id is not a uuid", ErrProviderUnavailable)
	}
	if !hasVerifiedEmail(session) {
		return Principal{}, ErrVerificationRequired
	}

	authenticatedAt, err := time.Parse(time.RFC3339Nano, session.AuthenticatedAt)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: invalid authenticated_at", ErrProviderUnavailable)
	}
	return Principal{UserID: userID.String(), AuthenticatedAt: authenticatedAt.UTC()}, nil
}

func hasVerifiedEmail(session whoamiSession) bool {
	for _, address := range session.Identity.VerifiableAddresses {
		if address.Via == "email" && address.Verified && address.Status == "completed" {
			return true
		}
	}
	return false
}
