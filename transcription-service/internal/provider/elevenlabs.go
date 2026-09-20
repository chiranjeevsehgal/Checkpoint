package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"transcription-service/internal/config"
	"transcription-service/internal/model"
)

const elevenLabsURL = "https://api.elevenlabs.io/v1/speech-to-text"

type ElevenLabsProvider struct {
	apiKey         string
	modelID        string
	diarize        bool
	tagAudioEvents bool
	maxAttempts    int
	client         *http.Client
}

func NewElevenLabsProvider(cfg config.ElevenLabsConfig) *ElevenLabsProvider {
	attempts := cfg.MaxAttempts
	if attempts <= 0 {
		attempts = 5
	}
	return &ElevenLabsProvider{
		apiKey:         cfg.APIKey(),
		modelID:        cfg.ModelID,
		diarize:        cfg.Diarize,
		tagAudioEvents: cfg.TagAudioEvents,
		maxAttempts:    attempts,
		client:         &http.Client{Timeout: 10 * time.Minute},
	}
}

func (e *ElevenLabsProvider) Name() string { return "elevenlabs" }

func (e *ElevenLabsProvider) Transcribe(ctx context.Context, audio []byte, contentType string, languages []string) (*model.TranscriptResult, error) {
	var lastErr error
	for attempt := 1; attempt <= e.maxAttempts; attempt++ {
		result, retryable, err := e.attempt(ctx, audio, contentType, languages)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
		if attempt == e.maxAttempts {
			break
		}
		backoff := time.Duration(1<<(attempt-1)) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, fmt.Errorf("elevenlabs failed after %d attempts: %w", e.maxAttempts, lastErr)
}

func (e *ElevenLabsProvider) attempt(ctx context.Context, audio []byte, contentType string, languages []string) (*model.TranscriptResult, bool, error) {
	body, boundary, err := buildMultipartBody(audio, contentType, e.modelID, e.diarize, e.tagAudioEvents, singleLanguage(languages))
	if err != nil {
		return nil, false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, elevenLabsURL, body)
	if err != nil {
		return nil, false, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("xi-api-key", e.apiKey)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result, err := parseElevenLabsResponse(respBody)
		if err != nil {
			return nil, false, err
		}
		return result, false, nil
	}

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return nil, false, fmt.Errorf("elevenlabs rejected audio: %d %s", resp.StatusCode, string(respBody))
	}

	return nil, true, fmt.Errorf("elevenlabs server error: %d %s", resp.StatusCode, string(respBody))
}

// singleLanguage forces language_code only when the user selected exactly one
// language; a multi-language selection must be auto-detected.
func singleLanguage(languages []string) string {
	if len(languages) == 1 {
		return languages[0]
	}
	return ""
}

func buildMultipartBody(audio []byte, contentType, modelID string, diarize, tagAudioEvents bool, languageCode string) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("model_id", modelID); err != nil {
		return nil, "", fmt.Errorf("writing model_id field: %w", err)
	}
	if languageCode != "" {
		if err := writer.WriteField("language_code", languageCode); err != nil {
			return nil, "", fmt.Errorf("writing language_code field: %w", err)
		}
	}
	if err := writer.WriteField("diarize", strconv.FormatBool(diarize)); err != nil {
		return nil, "", fmt.Errorf("writing diarize field: %w", err)
	}
	if err := writer.WriteField("tag_audio_events", strconv.FormatBool(tagAudioEvents)); err != nil {
		return nil, "", fmt.Errorf("writing tag_audio_events field: %w", err)
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileNameFor(contentType)))
	header.Set("Content-Type", normalizeContentType(contentType))
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", fmt.Errorf("creating file part: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", fmt.Errorf("writing audio bytes: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}
	return &body, writer.Boundary(), nil
}

func fileNameFor(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])) {
	case "audio/ogg", "application/ogg":
		return "audio.ogg"
	case "audio/mpeg", "audio/mp3":
		return "audio.mp3"
	case "audio/wav", "audio/x-wav":
		return "audio.wav"
	case "audio/webm":
		return "audio.webm"
	case "audio/mp4", "audio/m4a":
		return "audio.m4a"
	case "audio/flac":
		return "audio.flac"
	default:
		return "audio.bin"
	}
}

func normalizeContentType(contentType string) string {
	trimmed := strings.TrimSpace(contentType)
	if trimmed == "" {
		return "application/octet-stream"
	}
	return strings.SplitN(trimmed, ";", 2)[0]
}

type elevenLabsResponse struct {
	LanguageCode        string  `json:"language_code"`
	LanguageProbability float64 `json:"language_probability"`
	Text                string  `json:"text"`
	Words               []struct {
		Text      string  `json:"text"`
		Start     float64 `json:"start"`
		End       float64 `json:"end"`
		Type      string  `json:"type"`
		SpeakerID string  `json:"speaker_id"`
		LogProb   float64 `json:"logprob"`
	} `json:"words"`
	TranscriptionID   string  `json:"transcription_id"`
	AudioDurationSecs float64 `json:"audio_duration_secs"`
}

func parseElevenLabsResponse(body []byte) (*model.TranscriptResult, error) {
	var raw elevenLabsResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing elevenlabs response: %w", err)
	}

	return &model.TranscriptResult{
		Text:            raw.Text,
		Language:        raw.LanguageCode,
		DurationSeconds: raw.AudioDurationSecs,
		SpeakerSegments: groupWords(raw.Words),
		Provider:        "elevenlabs",
		RequestID:       raw.TranscriptionID,
	}, nil
}

func groupWords(words []struct {
	Text      string  `json:"text"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Type      string  `json:"type"`
	SpeakerID string  `json:"speaker_id"`
	LogProb   float64 `json:"logprob"`
}) []model.SpeakerSegment {
	segments := make([]model.SpeakerSegment, 0)
	var current *model.SpeakerSegment
	var logSum float64
	var logCount int

	flush := func() {
		if current == nil {
			return
		}
		if logCount > 0 {
			current.Confidence = math.Exp(logSum / float64(logCount))
		}
		segments = append(segments, *current)
		current = nil
		logSum = 0
		logCount = 0
	}

	for _, w := range words {
		if w.Type != "word" {
			continue
		}
		speaker := speakerIndex(w.SpeakerID)
		if current == nil || current.Speaker != speaker {
			flush()
			current = &model.SpeakerSegment{Speaker: speaker, Start: w.Start, End: w.End}
		}
		if current.Text == "" {
			current.Text = w.Text
		} else {
			current.Text += " " + w.Text
		}
		current.End = w.End
		logSum += w.LogProb
		logCount++
	}
	flush()

	if segments == nil {
		segments = make([]model.SpeakerSegment, 0)
	}
	return segments
}

func speakerIndex(speakerID string) int {
	id := strings.TrimSpace(speakerID)
	if rest, found := strings.CutPrefix(id, "speaker_"); found {
		if n, err := strconv.Atoi(rest); err == nil {
			return n
		}
	}
	if n, err := strconv.Atoi(id); err == nil {
		return n
	}
	return 0
}
