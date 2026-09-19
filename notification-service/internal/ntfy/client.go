package ntfy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Typed errors so the scheduler can distinguish retryable (429/5xx/network)
// from terminal (4xx) failures.
var (
	ErrRateLimited = errors.New("ntfy: rate limited")
	ErrServer      = errors.New("ntfy: server error")
	ErrBadRequest  = errors.New("ntfy: bad request")
)

// Options carries the client's connection settings and message controls.
type Options struct {
	BaseURL      string
	Token        string
	Timeout      time.Duration
	MaxBodyBytes int
}

type Client struct {
	http         *http.Client
	baseURL      string
	token        string
	maxBodyBytes int
}

func New(o Options) *Client {
	return &Client{
		http:         &http.Client{Timeout: o.Timeout},
		baseURL:      strings.TrimRight(o.BaseURL, "/"),
		token:        o.Token,
		maxBodyBytes: o.MaxBodyBytes,
	}
}

// Publish sends one notification to a topic. Title and priority are headers
// when set; the body is truncated so ntfy keeps it a message, not an
// attachment.
func (c *Client) Publish(ctx context.Context, topic, title, body, priority string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+topic,
		bytes.NewReader([]byte(truncate(body, c.maxBodyBytes))))
	if err != nil {
		return fmt.Errorf("building ntfy request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if title != "" {
		req.Header.Set("Title", title)
	}
	if priority != "" {
		req.Header.Set("Priority", priority)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return classifyError(resp)
}

func classifyError(resp *http.Response) error {
	msg := readError(resp.Body)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w (retry-after %s): %s", ErrRateLimited, resp.Header.Get("Retry-After"), msg)
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w (status %d): %s", ErrServer, resp.StatusCode, msg)
	default:
		return fmt.Errorf("%w (status %d): %s", ErrBadRequest, resp.StatusCode, msg)
	}
}

func readError(body io.Reader) string {
	const max = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(body, max))
	if err != nil {
		return "unreadable error body"
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		return "no error body"
	}
	return msg
}

// truncate caps body at maxBytes without splitting a UTF-8 rune.
func truncate(body string, maxBytes int) string {
	if maxBytes <= 0 || len(body) <= maxBytes {
		return body
	}
	cut := body[:maxBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
