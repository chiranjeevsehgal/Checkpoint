// Package service implements the ingestion application logic. Handlers
// decode HTTP, call one method here, and encode the result; no database
// or MinIO calls happen outside this package and the repositories.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/storage"
)

// UploadURLExpiry is how long a presigned PUT URL stays usable.
const UploadURLExpiry = 15 * time.Minute

// ErrSizeMismatch is returned when the stored object's size disagrees
// with the declared or reported size.
var ErrSizeMismatch = errors.New("size mismatch")

// UploadService orchestrates upload creation and completion.
type UploadService struct {
	uploads    repository.UploadRepository
	completion repository.UploadCompletionRepository
	storage    storage.ObjectStorage
	bucket     string
	now        func() time.Time
}

// NewUploadService wires the service. Pass nil for now to use UTC time.
func NewUploadService(
	uploads repository.UploadRepository,
	completion repository.UploadCompletionRepository,
	objectStorage storage.ObjectStorage,
	bucket string,
	now func() time.Time,
) *UploadService {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &UploadService{
		uploads:    uploads,
		completion: completion,
		storage:    objectStorage,
		bucket:     bucket,
		now:        now,
	}
}

// CreateCommand carries the POST /v1/uploads request fields.
type CreateCommand struct {
	Filename    string
	ContentType string
	SizeBytes   int64
	RecordedAt  *time.Time
	DeviceID    string
}

// CreateResult pairs the persisted upload with its presigned PUT URL.
type CreateResult struct {
	Upload    *domain.Upload
	UploadURL string
	ExpiresAt time.Time
}

// buildUpload normalizes the command once so validation, persistence,
// presigning and transcription payloads share one canonical form.
func (s *UploadService) buildUpload(userID string, cmd CreateCommand, now time.Time) *domain.Upload {
	filename, contentType := domain.NormalizeCreate(cmd.Filename, cmd.ContentType)
	uploadID := uuid.NewString()
	size := cmd.SizeBytes
	upload := &domain.Upload{
		ID:               uploadID,
		UserID:           userID,
		DeviceID:         cmd.DeviceID,
		Bucket:           s.bucket,
		ObjectKey:        domain.ObjectKeyFor(userID, uploadID, now),
		OriginalFilename: filename,
		ContentType:      contentType,
		ExpectedSize:     &size,
		Status:           domain.StatusUploading,
		RecordedAt:       cmd.RecordedAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	expiresAt := now.Add(UploadURLExpiry)
	upload.UploadExpiresAt = &expiresAt
	return upload
}

// CreateUpload validates, mints a presigned URL, and persists the
// UPLOADING row. The presigned URL is created before the row; an orphan
// URL on DB failure simply expires unused.
func (s *UploadService) CreateUpload(ctx context.Context, userID string, cmd CreateCommand) (*CreateResult, error) {
	if err := domain.ValidateCreate(cmd.Filename, cmd.ContentType, cmd.SizeBytes); err != nil {
		return nil, err
	}

	now := s.now()
	upload := s.buildUpload(userID, cmd, now)

	putURL, err := s.storage.CreateUploadURL(ctx, s.bucket, upload.ObjectKey, upload.ContentType, UploadURLExpiry)
	if err != nil {
		return nil, fmt.Errorf("presign upload: %w", err)
	}
	if err := s.uploads.Create(ctx, upload); err != nil {
		return nil, fmt.Errorf("persist upload: %w", err)
	}
	return &CreateResult{Upload: upload, UploadURL: putURL, ExpiresAt: *upload.UploadExpiresAt}, nil
}

// IdempotentCreateOutcome reports a keyed create. Replay is true when
// the key was already claimed; Stored then holds the original response.
// Body carries the fresh encoded response and is nil on replay.
type IdempotentCreateOutcome struct {
	Result *CreateResult
	Body   []byte
	Stored *repository.IdempotencyRecord
	Replay bool
}

// CreateUploadIdempotent validates, mints a presigned URL, then claims
// the idempotency key and persists the upload atomically. Presigning
// stays outside the transaction: it is offline crypto with no I/O, and
// an orphan URL on transaction failure simply expires unused. encode
// renders the exact response bytes stored for replay. On replay the
// stored upload_id is reused but a fresh URL is minted so retries after
// the 15m expiry still receive a usable URL.
func (s *UploadService) CreateUploadIdempotent(
	ctx context.Context,
	userID string,
	cmd CreateCommand,
	key, requestHash string,
	encode func(*CreateResult) []byte,
) (*IdempotentCreateOutcome, error) {
	if err := domain.ValidateCreate(cmd.Filename, cmd.ContentType, cmd.SizeBytes); err != nil {
		return nil, err
	}

	now := s.now()
	upload := s.buildUpload(userID, cmd, now)

	putURL, err := s.storage.CreateUploadURL(ctx, s.bucket, upload.ObjectKey, upload.ContentType, UploadURLExpiry)
	if err != nil {
		return nil, fmt.Errorf("presign upload: %w", err)
	}
	res := &CreateResult{Upload: upload, UploadURL: putURL, ExpiresAt: *upload.UploadExpiresAt}
	body := encode(res)

	stored, err := s.uploads.CreateUploadIdempotent(ctx, repository.IdempotentCreateParams{
		Upload:         upload,
		Key:            key,
		UserID:         userID,
		DeviceID:       upload.DeviceID,
		RequestHash:    requestHash,
		ResponseStatus: 201,
		ResponseBody:   body,
	})
	if err != nil {
		return nil, fmt.Errorf("persist upload: %w", err)
	}
	if stored.Replay {
		freshBody, freshRes := s.refreshReplay(ctx, userID, stored.Stored, encode)
		if freshRes != nil {
			return &IdempotentCreateOutcome{Stored: stored.Stored, Result: freshRes, Body: freshBody, Replay: true}, nil
		}
		return &IdempotentCreateOutcome{Stored: stored.Stored, Replay: true}, nil
	}
	return &IdempotentCreateOutcome{Result: res, Body: body}, nil
}

// refreshReplay mints a fresh presigned URL for the winner upload so
// retries after expiry still work. It returns nil when the upload row
// cannot be resolved, letting the caller fall back to the stored body.
func (s *UploadService) refreshReplay(ctx context.Context, userID string, stored *repository.IdempotencyRecord, encode func(*CreateResult) []byte) ([]byte, *CreateResult) {
	if stored == nil {
		return nil, nil
	}
	uploadID := parseUploadID(stored.ResponseBody)
	if uploadID == "" {
		return nil, nil
	}
	upload, err := s.uploads.GetByIDForUser(ctx, userID, uploadID)
	if err != nil {
		return nil, nil
	}
	now := s.now()
	freshURL, err := s.storage.CreateUploadURL(ctx, upload.Bucket, upload.ObjectKey, upload.ContentType, UploadURLExpiry)
	if err != nil {
		return nil, nil
	}
	expiresAt := now.Add(UploadURLExpiry)
	fresh := &CreateResult{Upload: upload, UploadURL: freshURL, ExpiresAt: expiresAt}
	return encode(fresh), fresh
}

func parseUploadID(body []byte) string {
	var v struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	return strings.TrimSpace(v.UploadID)
}

// CompleteCommand carries the optional /complete confirmation fields.
// A zero SizeBytes means the client did not report a size.
type CompleteCommand struct {
	SizeBytes int64
	Checksum  string
}

// CompleteUpload verifies the MinIO object and flips the upload to READY
// with its transcription event in one transaction. Repeats for READY/SUBMITTED
// uploads succeed without touching MinIO.
func (s *UploadService) CompleteUpload(ctx context.Context, userID, uploadID string, cmd CompleteCommand) (*domain.Upload, error) {
	if cmd.SizeBytes < 0 {
		return nil, domain.ErrInvalidSize
	}
	if err := domain.ValidateChecksumFormat(cmd.Checksum); err != nil {
		return nil, err
	}
	upload, err := s.uploads.GetByIDForUser(ctx, userID, uploadID)
	if err != nil {
		return nil, err
	}
	if domain.IsCompletionIdempotent(upload.Status) {
		return upload, nil
	}
	if !domain.CanTransitionToComplete(upload.Status) {
		return nil, repository.ErrInvalidState
	}

	info, err := s.storage.StatObject(ctx, upload.Bucket, upload.ObjectKey)
	if err != nil {
		return nil, err
	}
	if info.Size <= 0 {
		return nil, fmt.Errorf("%w: object is %d bytes", ErrSizeMismatch, info.Size)
	}
	if info.Size > domain.MaxUploadBytes {
		return nil, fmt.Errorf("%w: object is %d bytes exceeds %d", domain.ErrTooLarge, info.Size, domain.MaxUploadBytes)
	}
	if actual := strings.ToLower(strings.TrimSpace(info.ContentType)); actual != "" {
		if _, normalized := domain.NormalizeCreate("", upload.ContentType); actual != normalized {
			return nil, fmt.Errorf("%w: declared %q but object is %q", domain.ErrUnsupportedMediaType, upload.ContentType, info.ContentType)
		}
	}
	if cmd.SizeBytes != 0 && cmd.SizeBytes != info.Size {
		return nil, fmt.Errorf("%w: client reported %d, object is %d", ErrSizeMismatch, cmd.SizeBytes, info.Size)
	}
	if upload.ExpectedSize != nil && *upload.ExpectedSize != info.Size {
		return nil, fmt.Errorf("%w: declared %d, object is %d", ErrSizeMismatch, *upload.ExpectedSize, info.Size)
	}

	now := s.now()
	eventID := uuid.NewString()
	payload, err := json.Marshal(domain.NewAudioReadyPayload(eventID, upload, info.Size, cmd.Checksum, now))
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	res, err := s.completion.MarkReadyAndCreateEvent(ctx, repository.CompleteParams{
		UploadID:   upload.ID,
		UserID:     userID,
		ActualSize: info.Size,
		Checksum:   cmd.Checksum,
		EventID:    eventID,
		Payload:    payload,
		Now:        now,
	})
	if err != nil {
		return nil, err
	}
	return res.Upload, nil
}

// GetUpload returns one upload enforcing ownership.
func (s *UploadService) GetUpload(ctx context.Context, userID, uploadID string) (*domain.Upload, error) {
	return s.uploads.GetByIDForUser(ctx, userID, uploadID)
}
