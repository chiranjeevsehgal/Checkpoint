package provider

import (
	"context"

	"transcription-service/internal/model"
)

// Transcriber is the contract every STT provider must satisfy.
// Swapping Deepgram for anything else means writing a new implementation of this interface — nothing else in the service changes.
type Transcriber interface {
	// Transcribe takes raw audio bytes, its content type and the user's allowed
	// ISO-639-3 languages, and returns a normalized TranscriptResult. An empty
	// language set means auto-detect with no hint. Provider-specific parsing
	// happens inside the implementation so callers never see raw provider JSON.
	Transcribe(ctx context.Context, audio []byte, contentType string, languages []string) (*model.TranscriptResult, error)

	// Name identifies the provider, used for logging and stamping results.
	Name() string
}