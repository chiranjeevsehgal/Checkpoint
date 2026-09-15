package provider

import (
	"strings"
	"testing"
)

func TestSingleLanguage(t *testing.T) {
	if got := singleLanguage(nil); got != "" {
		t.Fatalf("nil: got %q, want empty", got)
	}
	if got := singleLanguage([]string{"eng"}); got != "eng" {
		t.Fatalf("single: got %q, want eng", got)
	}
	if got := singleLanguage([]string{"eng", "hin"}); got != "" {
		t.Fatalf("multiple: got %q, want empty", got)
	}
}

func TestBuildMultipartBodyLanguageCode(t *testing.T) {
	body, _, err := buildMultipartBody([]byte("audio"), "audio/ogg", "scribe_v2", true, true, "eng")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(body.String(), `name="language_code"`) || !strings.Contains(body.String(), "eng") {
		t.Fatalf("language_code field missing:\n%s", body.String())
	}

	body, _, err = buildMultipartBody([]byte("audio"), "audio/ogg", "scribe_v2", true, true, "")
	if err != nil {
		t.Fatalf("build empty: %v", err)
	}
	if strings.Contains(body.String(), `name="language_code"`) {
		t.Fatalf("language_code must be omitted when empty:\n%s", body.String())
	}
}

func TestParseElevenLabsResponse(t *testing.T) {
	raw := []byte(`{
		"language_code": "eng",
		"text": "hello world",
		"audio_duration_secs": 1.5,
		"transcription_id": "t-1",
		"words": [
			{"text": "hello", "start": 0, "end": 0.5, "type": "word", "speaker_id": "speaker_0", "logprob": -0.1},
			{"text": "world", "start": 0.5, "end": 1.0, "type": "word", "speaker_id": "speaker_0", "logprob": -0.2}
		]
	}`)
	result, err := parseElevenLabsResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Language != "eng" || result.Text != "hello world" || result.DurationSeconds != 1.5 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.SpeakerSegments) != 1 || result.SpeakerSegments[0].Text != "hello world" {
		t.Fatalf("segments: %+v", result.SpeakerSegments)
	}
}
