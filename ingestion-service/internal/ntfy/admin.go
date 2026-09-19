// Package ntfy talks to the self-hosted ntfy admin API to provision per-user
// read access for reminder notifications.
package ntfy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Typed errors so callers can distinguish auth, availability and bad input.
var (
	ErrUnauthorized = errors.New("ntfy: unauthorized")
	ErrUnavailable  = errors.New("ntfy: unavailable")
	ErrBadRequest   = errors.New("ntfy: bad request")
)

type AdminOptions struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

// AdminClient provisions ntfy users, ACL entries and access tokens.
type AdminClient struct {
	http    *http.Client
	baseURL string
	token   string
}

func NewAdmin(o AdminOptions) *AdminClient {
	return &AdminClient{
		http:    &http.Client{Timeout: o.Timeout},
		baseURL: strings.TrimRight(o.BaseURL, "/"),
		token:   o.Token,
	}
}

// EnsureUser creates the user; an already-existing user is not an error, so a
// re-enabled channel can reuse its identity.
func (c *AdminClient) EnsureUser(ctx context.Context, username, password string) error {
	status, body, err := c.do(ctx, http.MethodPut, "/v1/users", c.bearer(),
		map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	if status == http.StatusConflict {
		return nil
	}
	return checkStatus("create user", status, body)
}

// GrantRead grants the user read-only access to one topic.
func (c *AdminClient) GrantRead(ctx context.Context, username, topic string) error {
	status, body, err := c.do(ctx, http.MethodPost, "/v1/users/access", c.bearer(),
		map[string]string{"username": username, "topic": topic, "permission": "ro"})
	if err != nil {
		return err
	}
	return checkStatus("grant read", status, body)
}

// RevokeAccess removes the user's access to a topic; a missing grant is fine.
func (c *AdminClient) RevokeAccess(ctx context.Context, username, topic string) error {
	status, body, err := c.do(ctx, http.MethodDelete, "/v1/users/access", c.bearer(),
		map[string]string{"username": username, "topic": topic})
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	return checkStatus("revoke access", status, body)
}

// DeleteUser removes the user; a missing user is fine.
func (c *AdminClient) DeleteUser(ctx context.Context, username string) error {
	status, body, err := c.do(ctx, http.MethodDelete, "/v1/users", c.bearer(),
		map[string]string{"username": username})
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	return checkStatus("delete user", status, body)
}

// MintToken returns an access token for the user, authenticating with the
// user's password.
func (c *AdminClient) MintToken(ctx context.Context, username, password string) (string, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/v1/account/token", basicAuth(username, password),
		map[string]string{})
	if err != nil {
		return "", err
	}
	if err := checkStatus("create token", status, body); err != nil {
		return "", err
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("ntfy: decoding token response: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("%w: empty token", ErrUnavailable)
	}
	return out.Token, nil
}

func (c *AdminClient) bearer() string { return "Bearer " + c.token }

func (c *AdminClient) do(ctx context.Context, method, path, auth string, payload any) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("ntfy: encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("ntfy: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, respBody, nil
}

func checkStatus(action string, status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%w: %s (%d): %s", ErrUnauthorized, action, status, message)
	case status >= 500:
		return fmt.Errorf("%w: %s (%d): %s", ErrUnavailable, action, status, message)
	default:
		return fmt.Errorf("%w: %s (%d): %s", ErrBadRequest, action, status, message)
	}
}

func basicAuth(username, password string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return "Basic " + encoded
}
