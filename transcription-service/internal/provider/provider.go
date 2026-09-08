package provider

import (
	"context"

	"transcription-service/internal/model"
)

// Transcriber is the contract every STT provider must satisfy.
// Swapping Deepgram for anything else means writing a new implementation of this interface — nothing else in the service changes.
type Transcriber interface {
	// Transcribe takes raw audio bytes and its content type, and returns a normalized TranscriptResult. Provider-specific parsing happens inside the implementation so callers never see raw provider JSON.
	Transcribe(ctx context.Context, audio []byte, contentType string) (*model.TranscriptResult, error)

	// Name identifies the provider, used for logging and stamping results.
	Name() string
}