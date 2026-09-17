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
	"net/url"
	"strconv"
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
// authorization only needs the immutable identity, the current session and
// its age.
type Principal struct {
	UserID          string
	SessionID       string
	AuthenticatedAt time.Time
	// EmailVerifiedAt is the most recent email verification time, used as a
	// step-up for destructive account actions.
	EmailVerifiedAt time.Time
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
	ID              string `json:"id"`
	Active          bool   `json:"active"`
	AuthenticatedAt string `json:"authenticated_at"`
	Identity        struct {
		ID                  string              `json:"id"`
		VerifiableAddresses []VerifiableAddress `json:"verifiable_addresses"`
	} `json:"identity"`
}

// VerifiableAddress is a Kratos address that can complete verification.
type VerifiableAddress struct {
	Via        string  `json:"via"`
	Verified   bool    `json:"verified"`
	Status     string  `json:"status"`
	VerifiedAt *string `json:"verified_at"`
}

// HasVerifiedEmail reports whether any email address completed verification.
func HasVerifiedEmail(addresses []VerifiableAddress) bool {
	for _, address := range addresses {
		if address.Via == "email" && address.Verified && address.Status == "completed" {
			return true
		}
	}
	return false
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
	return Principal{
		UserID:          userID.String(),
		SessionID:       session.ID,
		AuthenticatedAt: authenticatedAt.UTC(),
		EmailVerifiedAt: latestEmailVerification(session).UTC(),
	}, nil
}

// latestEmailVerification returns the most recent verified_at across completed
// email addresses, or the zero time when none is present.
func latestEmailVerification(session whoamiSession) time.Time {
	var latest time.Time
	for _, address := range session.Identity.VerifiableAddresses {
		if address.Via != "email" || !address.Verified ||
			address.Status != "completed" || address.VerifiedAt == nil {
			continue
		}
		verifiedAt, err := time.Parse(time.RFC3339Nano, *address.VerifiedAt)
		if err != nil {
			continue
		}
		if verifiedAt.After(latest) {
			latest = verifiedAt
		}
	}
	return latest
}

// IdentityDeleter removes identities through the private Kratos Admin API.
type IdentityDeleter interface {
	DeleteIdentity(ctx context.Context, identityID string) error
}

// Identity is the subset of a Kratos identity needed for lifecycle cleanup.
type Identity struct {
	ID                  string              `json:"id"`
	State               string              `json:"state"`
	CreatedAt           time.Time           `json:"created_at"`
	VerifiableAddresses []VerifiableAddress `json:"verifiable_addresses"`
}

// KratosAdmin is the private-infrastructure client for identity deletion.
type KratosAdmin struct {
	adminURL string
	client   *http.Client
}

// NewKratosAdmin builds a client for the Kratos admin API.
func NewKratosAdmin(adminURL string, timeout time.Duration) *KratosAdmin {
	return &KratosAdmin{
		adminURL: strings.TrimRight(adminURL, "/"),
		client:   &http.Client{Timeout: timeout},
	}
}

// ListIdentities returns one page of identities and the token for the next
// page (empty when there are no more pages).
func (a *KratosAdmin) ListIdentities(ctx context.Context, pageSize int, pageToken string) ([]Identity, string, error) {
	u, err := url.Parse(a.adminURL + "/admin/identities")
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	query := u.Query()
	query.Set("page_size", strconv.Itoa(pageSize))
	if pageToken != "" {
		query.Set("page_token", pageToken)
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("%w: kratos admin status %d", ErrProviderUnavailable, resp.StatusCode)
	}
	var identities []Identity
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&identities); err != nil {
		return nil, "", fmt.Errorf("%w: decode identities: %v", ErrProviderUnavailable, err)
	}
	return identities, nextPageToken(resp.Header.Get("Link")), nil
}

// nextPageToken extracts page_token from a Link header entry with rel="next".
func nextPageToken(header string) string {
	for _, part := range strings.Split(header, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start, end := strings.Index(part, "<"), strings.Index(part, ">")
		if start < 0 || end <= start {
			continue
		}
		u, err := url.Parse(part[start+1 : end])
		if err != nil {
			continue
		}
		return u.Query().Get("page_token")
	}
	return ""
}

// DeleteIdentity deletes an identity and its sessions. A missing identity
// is treated as already deleted.
func (a *KratosAdmin) DeleteIdentity(ctx context.Context, identityID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.adminURL+"/admin/identities/"+identityID, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: kratos admin status %d", ErrProviderUnavailable, resp.StatusCode)
	}
	return nil
}

// RevokeOtherSessions invalidates every session of an identity except the
// one making the call. A missing identity is treated as already revoked.
func (a *KratosAdmin) RevokeOtherSessions(ctx context.Context, identityID, keepSessionID string) error {
	sessions, err := a.listIdentitySessions(ctx, identityID)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.ID == keepSessionID {
			continue
		}
		if err := a.disableSession(ctx, session.ID); err != nil {
			return err
		}
	}
	return nil
}

type adminSession struct {
	ID string `json:"id"`
}

func (a *KratosAdmin) listIdentitySessions(ctx context.Context, identityID string) ([]adminSession, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.adminURL+"/admin/identities/"+identityID+"/sessions", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: kratos admin status %d", ErrProviderUnavailable, resp.StatusCode)
	}
	var sessions []adminSession
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("%w: decode sessions: %v", ErrProviderUnavailable, err)
	}
	return sessions, nil
}

func (a *KratosAdmin) disableSession(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.adminURL+"/admin/sessions/"+sessionID, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: kratos admin status %d", ErrProviderUnavailable, resp.StatusCode)
	}
	return nil
}

func hasVerifiedEmail(session whoamiSession) bool {
	return HasVerifiedEmail(session.Identity.VerifiableAddresses)
}
