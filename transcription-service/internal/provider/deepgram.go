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
	"transcription-service/internal/language"
	"transcription-service/internal/model"
)

const deepgramURL = "https://api.deepgram.com/v1/listen"

type DeepgramProvider struct {
	apiKey      string
	model       string
	language    string
	options     map[string]string
	maxAttempts int
	client      *http.Client
}

func NewDeepgramProvider(cfg config.DeepgramConfig) *DeepgramProvider {
	attempts := cfg.MaxAttempts
	if attempts <= 0 {
		attempts = 5
	}
	return &DeepgramProvider{
		apiKey:      cfg.APIKey(),
		model:       cfg.Model,
		language:    cfg.Language,
		options:     cfg.Options,
		maxAttempts: attempts,
		client:      &http.Client{Timeout: 10 * time.Minute},
	}
}

func (d *DeepgramProvider) Name() string { return "deepgram" }

// deepgramForcedLanguages are the ISO-639-1 codes nova-3 accepts via the
// `language` parameter.
var deepgramForcedLanguages = languageSet(
	"af", "ar", "hy", "as", "be", "bn", "bs", "bg", "ca", "zh", "hr", "cs",
	"da", "nl", "en", "et", "fi", "fr", "ka", "de", "el", "gu", "he", "hi",
	"hu", "id", "it", "ja", "kn", "kk", "ko", "lv", "lt", "mk", "ms", "mr",
	"mn", "ne", "no", "ps", "fa", "pl", "pt", "pa", "ro", "ru", "sr", "sk",
	"sl", "es", "sv", "tl", "ta", "te", "th", "tr", "uk", "ur", "vi",
)

// deepgramDetectLanguages are the codes Deepgram's detect_language supports.
var deepgramDetectLanguages = languageSet(
	"bg", "ca", "cs", "da", "de", "el", "en", "es", "et", "fi", "fr", "hi",
	"hu", "id", "it", "ja", "ko", "lt", "lv", "ms", "nl", "no", "pl", "pt",
	"ro", "ru", "sk", "sv", "th", "tr", "uk", "vi", "zh",
)

func languageSet(codes ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	return set
}

// deepgramParams builds one request's query. A single supported language is
// forced; several all-supported languages restrict detection; anything else
// falls back to the configured language and is filtered downstream.
func deepgramParams(model, fallbackLanguage string, options map[string]string, selected []string) url.Values {
	params := url.Values{}
	params.Set("model", model)

	hints := make([]string, 0, len(selected))
	for _, code := range selected {
		hints = append(hints, language.ISO1(code))
	}

	switch {
	case len(hints) == 1 && deepgramSupports(hints[0], deepgramForcedLanguages):
		params.Set("language", hints[0])
	case len(hints) > 1 && deepgramSupportsAll(hints, deepgramDetectLanguages):
		for _, hint := range hints {
			params.Add("detect_language", hint)
		}
	default:
		if fallbackLanguage != "" {
			params.Set("language", fallbackLanguage)
		}
	}

	for key, value := range options {
		params.Set(key, value)
	}
	return params
}

func deepgramSupports(code string, set map[string]struct{}) bool {
	if code == "" {
		return false
	}
	_, ok := set[code]
	return ok
}

func deepgramSupportsAll(codes []string, set map[string]struct{}) bool {
	for _, code := range codes {
		if !deepgramSupports(code, set) {
			return false
		}
	}
	return true
}

func (d *DeepgramProvider) Transcribe(ctx context.Context, audio []byte, contentType string, languages []string) (*model.TranscriptResult, error) {
	params := deepgramParams(d.model, d.language, d.options, languages)

	var lastErr error
	for attempt := 1; attempt <= d.maxAttempts; attempt++ {
		result, retryable, err := d.attempt(ctx, audio, contentType, params)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
		if attempt == d.maxAttempts {
			break
		}
		backoff := time.Duration(1<<(attempt-1)) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, fmt.Errorf("deepgram failed after %d attempts: %w", d.maxAttempts, lastErr)
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
				Transcript string   `json:"transcript"`
				Languages  []string `json:"languages"`
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
	language := channel.DetectedLanguage
	if len(channel.Alternatives) > 0 {
		alt := channel.Alternatives[0]
		transcript = alt.Transcript
		if language == "" && len(alt.Languages) > 0 {
			language = alt.Languages[0]
		}
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
		Language:        language,
		DurationSeconds: raw.Metadata.Duration,
		SpeakerSegments: segments,
		Provider:        "deepgram",
		RequestID:       raw.Metadata.RequestID,
	}, nil
}