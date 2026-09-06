// Package vadclient submits accepted audio jobs to the VAD service over
// internal HTTP. Delivery is at-least-once: VAD deduplicates on EventID,
// so dispatcher retries are always safe.
package vadclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RequestTimeout bounds a single submission. VAD only needs to accept
// the job, so this stays short; backoff and retries belong to the
// outbox dispatcher.
const RequestTimeout = 5 * time.Second

// JobRequest is one VAD job submission.
type JobRequest struct {
	EventID     string
	AudioID     string
	Bucket      string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
}

type jobPayload struct {
	EventID     string    `json:"event_id"`
	AudioID     string    `json:"audio_id"`
	Object      objectRef `json:"object"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
}

type objectRef struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

// Client posts jobs to POST {baseURL}/internal/v1/jobs.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a client for a base URL like http://vad:8081.
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: RequestTimeout},
	}
}

// SubmitJob delivers one job. A nil error means VAD durably accepted it
// (200/202); anything else must be retried by the caller.
func (c *Client) SubmitJob(ctx context.Context, req JobRequest) error {
	body, err := json.Marshal(jobPayload{
		EventID:     req.EventID,
		AudioID:     req.AudioID,
		Object:      objectRef{Bucket: req.Bucket, Key: req.ObjectKey},
		ContentType: req.ContentType,
		SizeBytes:   req.SizeBytes,
	})
	if err != nil {
		return fmt.Errorf("marshal vad job: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/internal/v1/jobs", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build vad request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("post vad job: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("vad rejected job: status %s", resp.Status)
	}
	return nil
}
