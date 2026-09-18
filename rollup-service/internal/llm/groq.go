package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Typed errors so the caller can distinguish retryable (429/5xx/network) from
// terminal (4xx — bad request, auth) failures.
var (
	ErrRateLimited = errors.New("groq: rate limited")
	ErrBadRequest  = errors.New("groq: bad request")
	ErrServer      = errors.New("groq: server error")
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Options carries the client's connection settings and generation controls.
type Options struct {
	BaseURL             string
	APIKey              string
	Model               string
	MaxCompletionTokens int
	Temperature         float64
	TopP                float64
	ReasoningEffort     string
	Timeout             time.Duration
}

type Client struct {
	http                *http.Client
	baseURL             string
	apiKey              string
	model               string
	maxCompletionTokens int
	temperature         float64
	topP                float64
	reasoningEffort     string
}

func New(o Options) *Client {
	return &Client{
		http:                &http.Client{Timeout: o.Timeout},
		baseURL:             o.BaseURL,
		apiKey:              o.APIKey,
		model:               o.Model,
		maxCompletionTokens: o.MaxCompletionTokens,
		temperature:         o.Temperature,
		topP:                o.TopP,
		reasoningEffort:     o.ReasoningEffort,
	}
}

// Model returns the configured model name — recorded on persisted rows so
// audits know what produced them.
func (c *Client) Model() string { return c.model }

// Chat calls POST {baseURL}/chat/completions and returns the assistant message
// text (reasoning tokens, if any, stay server-side and are ignored).
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	payload := map[string]interface{}{
		"model":                 c.model,
		"messages":              messages,
		"max_completion_tokens": c.maxCompletionTokens,
	}
	if c.temperature > 0 {
		payload["temperature"] = c.temperature
	}
	if c.topP > 0 {
		payload["top_p"] = c.topP
	}
	if c.reasoningEffort != "" {
		payload["reasoning_effort"] = c.reasoningEffort
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshalling chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", classifyError(resp)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("decoding groq response: %w", err)
	}
	if len(out.Choices) == 0 {
		if out.Error != nil {
			return "", fmt.Errorf("%w: %s", ErrServer, out.Error.Message)
		}
		return "", fmt.Errorf("%w: empty choices", ErrServer)
	}
	content := out.Choices[0].Message.Content
	if content == "" {
		// Reasoning models occasionally put the whole answer in reasoning on
		// misconfigured requests; treat empty content as a server error.
		return "", fmt.Errorf("%w: empty content", ErrServer)
	}
	return content, nil
}

func classifyError(resp *http.Response) error {
	var body struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
	msg := "no error body"
	if body.Error != nil {
		msg = body.Error.Message
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w (retry-after %s): %s", ErrRateLimited, resp.Header.Get("Retry-After"), msg)
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w (status %d): %s", ErrServer, resp.StatusCode, msg)
	default:
		return fmt.Errorf("%w (status %d): %s", ErrBadRequest, resp.StatusCode, msg)
	}
}
