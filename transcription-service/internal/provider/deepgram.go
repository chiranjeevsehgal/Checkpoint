package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"transcription-service/internal/config"
	"transcription-service/internal/model"
)

const deepgramURL = "https://api.deepgram.com/v1/listen"

const maxAttempts = 5

type DeepgramProvider struct {
	apiKey   string
	model    string
	language string
	options  map[string]string
	client   *http.Client
}

func NewDeepgramProvider(cfg config.DeepgramConfig) *DeepgramProvider {
	return &DeepgramProvider{
		apiKey:   cfg.APIKey(),
		model:    cfg.Model,
		language: cfg.Language,
		options:  cfg.Options,
		client:   &http.Client{Timeout: 10 * time.Minute},
	}
}

func (d *DeepgramProvider) Name() string { return "deepgram" }

func (d *DeepgramProvider) Transcribe(ctx context.Context, audio []byte, contentType string) (*model.TranscriptResult, error) {
	params := url.Values{}
	params.Set("model", d.model)
	if d.language != "" {
		params.Set("language", d.language)
	}
	for k, v := range d.options {
		params.Set(k, v)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, retryable, err := d.attempt(ctx, audio, contentType, params)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
		if attempt == maxAttempts {
			break
		}
		backoff := time.Duration(1<<(attempt-1)) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, fmt.Errorf("deepgram failed after %d attempts: %w", maxAttempts, lastErr)
}

// attempt makes a single request. The bool return indicates whether the error (if any) is worth retrying — 4xx errors are not, 5xx/network errors are.
func (d *DeepgramProvider) attempt(ctx context.Context, audio []byte, contentType string, params url.Values) (*model.TranscriptResult, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		deepgramURL+"?"+params.Encode(), bytes.NewReader(audio))
	if err != nil {
		return nil, false, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+d.apiKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result, err := parseDeepgramResponse(body)
		if err != nil {
			return nil, false, err
		}
		return result, false, nil
	}

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return nil, false, fmt.Errorf("deepgram rejected audio: %d %s", resp.StatusCode, string(body))
	}

	return nil, true, fmt.Errorf("deepgram server error: %d %s", resp.StatusCode, string(body))
}

// --- Deepgram raw response shapes, used only for unmarshalling ---

type deepgramResponse struct {
	Metadata struct {
		Duration  float64 `json:"duration"`
		RequestID string  `json:"request_id"`
	} `json:"metadata"`
	Results struct {
		Channels []struct {
			DetectedLanguage string `json:"detected_language"`
			Alternatives     []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
		Utterances []struct {
			Speaker    int     `json:"speaker"`
			Start      float64 `json:"start"`
			End        float64 `json:"end"`
			Confidence float64 `json:"confidence"`
			Transcript string  `json:"transcript"`
		} `json:"utterances"`
	} `json:"results"`
}

func parseDeepgramResponse(body []byte) (*model.TranscriptResult, error) {
	var raw deepgramResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing deepgram response: %w", err)
	}
	if len(raw.Results.Channels) == 0 {
		return nil, fmt.Errorf("deepgram response has no channels")
	}
	channel := raw.Results.Channels[0]

	var transcript string
	if len(channel.Alternatives) > 0 {
		transcript = channel.Alternatives[0].Transcript
	}

	segments := make([]model.SpeakerSegment, 0, len(raw.Results.Utterances))
	for _, u := range raw.Results.Utterances {
		segments = append(segments, model.SpeakerSegment{
			Speaker:    u.Speaker,
			Start:      u.Start,
			End:        u.End,
			Confidence: u.Confidence,
			Text:       u.Transcript,
		})
	}

	return &model.TranscriptResult{
		Text:            transcript,
		Language:        channel.DetectedLanguage,
		DurationSeconds: raw.Metadata.Duration,
		SpeakerSegments: segments,
		Provider:        "deepgram",
		RequestID:       raw.Metadata.RequestID,
	}, nil
}