package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/service"
)

// uploadService is the subset of service.UploadService used by HTTP.
// An interface keeps handlers unit-testable without a database.
type uploadService interface {
	CreateUpload(ctx context.Context, userID string, cmd service.CreateCommand) (*service.CreateResult, error)
	CreateUploadIdempotent(ctx context.Context, userID string, cmd service.CreateCommand, key, requestHash string, encode func(*service.CreateResult) []byte) (*service.IdempotentCreateOutcome, error)
	CompleteUpload(ctx context.Context, userID, uploadID string, cmd service.CompleteCommand) (*domain.Upload, error)
	GetUpload(ctx context.Context, userID, uploadID string) (*domain.Upload, error)
}

// Handler serves the upload API. It decodes HTTP, calls the service and
// encodes the result; it holds no business logic. Idempotency replay is
// owned by the transactional service; idem is retained for wiring
// compatibility and future direct lookups.
type Handler struct {
	uploads   uploadService
	idem      repository.IdempotencyRepository
	created   *metrics.Counter
	completed *metrics.Counter
}

// NewHandler wires the upload endpoints.
func NewHandler(uploads uploadService, idem repository.IdempotencyRepository, reg *metrics.Registry) *Handler {
	_ = idem
	return &Handler{
		uploads:   uploads,
		idem:      idem,
		created:   reg.Counter("uploads_created_total"),
		completed: reg.Counter("uploads_completed_total", "status"),
	}
}

type createRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type createResponse struct {
	UploadID string `json:"upload_id"`
	Status   string `json:"status"`
	Upload   struct {
		Method    string `json:"method"`
		URL       string `json:"url"`
		ExpiresAt string `json:"expires_at"`
	} `json:"upload"`
}

// CreateUpload handles POST /v1/uploads. With an Idempotency-Key header,
// a retried request replays the original upload_id with a fresh URL
// instead of creating a second upload.
func (h *Handler) CreateUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}

	raw, ok := readRawBody(w, r, 1<<20)
	if !ok {
		return
	}
	var req createRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if key != "" {
		h.createIdempotent(w, r, principal.UserID, key, req)
		return
	}

	res, err := h.uploads.CreateUpload(r.Context(), principal.UserID, service.CreateCommand{
		Filename:    req.Filename,
		ContentType: req.ContentType,
		SizeBytes:   req.SizeBytes,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	body := encodeCreateResponse(res)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(body)
	h.created.Inc()
}

// createIdempotent handles a keyed POST /v1/uploads. The key claim and
// the upload insert happen in one transaction, so concurrent retries
// collapse to a single upload; persistence errors fail the request so
// the client can safely retry. Replay returns the same upload_id with
// a freshly minted URL.
func (h *Handler) createIdempotent(w http.ResponseWriter, r *http.Request, userID, key string, req createRequest) {
	if err := domain.ValidateIdempotencyKey(key); err != nil {
		writeServiceError(w, r, err)
		return
	}
	reqHash := hashCreateCommand(req)
	out, err := h.uploads.CreateUploadIdempotent(r.Context(), userID, service.CreateCommand{
		Filename:    req.Filename,
		ContentType: req.ContentType,
		SizeBytes:   req.SizeBytes,
	}, key, reqHash, encodeCreateResponse)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if out.Replay {
		if out.Stored == nil || out.Stored.RequestHash != reqHash {
			writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Idempotency-Key was already used with a different request.")
			return
		}
		body := out.Stored.ResponseBody
		if len(out.Body) > 0 {
			body = out.Body
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Stored.ResponseStatus)
		_, _ = w.Write(body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(out.Body)
	h.created.Inc()
}

// encodeCreateResponse renders the exact bytes stored for idempotency
// replay and written to the client.
func encodeCreateResponse(res *service.CreateResult) []byte {
	resp := createResponse{UploadID: res.Upload.ID, Status: res.Upload.Status}
	resp.Upload.Method = "PUT"
	resp.Upload.URL = res.UploadURL
	resp.Upload.ExpiresAt = res.ExpiresAt.UTC().Format(time.RFC3339)
	body, _ := json.Marshal(resp)
	return body
}

type completeRequest struct {
	SizeBytes *int64  `json:"size_bytes,omitempty"`
	Checksum  *string `json:"checksum_sha256,omitempty"`
}

type completeResponse struct {
	UploadID string `json:"upload_id"`
	Status   string `json:"status"`
}

// CompleteUpload handles POST /v1/uploads/{id}/complete. It is
// idempotent: repeats for READY or SUBMITTED uploads succeed.
func (h *Handler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	uploadID := r.PathValue("id")
	if _, err := uuid.Parse(uploadID); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Invalid upload ID.")
		return
	}

	var req completeRequest
	if r.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		_ = r.Body.Close()
		if err != nil {
			writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
			return
		}
		if int64(len(raw)) > 1<<20 {
			writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Request body too large.")
			return
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &req); err != nil {
				writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
				return
			}
		}
	}

	var cmd service.CompleteCommand
	if req.SizeBytes != nil {
		cmd.SizeBytes = *req.SizeBytes
	}
	if req.Checksum != nil {
		cmd.Checksum = *req.Checksum
	}
	upload, err := h.uploads.CompleteUpload(r.Context(), principal.UserID, uploadID, cmd)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	h.completed.Inc(upload.Status)
	writeJSON(w, http.StatusOK, completeResponse{UploadID: upload.ID, Status: upload.Status})
}

type getResponse struct {
	UploadID   string  `json:"upload_id"`
	Filename   string  `json:"filename"`
	SizeBytes  *int64  `json:"size_bytes"`
	Content    string  `json:"content_type"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	UploadedAt *string `json:"uploaded_at,omitempty"`
}

// GetUpload handles GET /v1/uploads/{id}.
func (h *Handler) GetUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
		return
	}
	uploadID := r.PathValue("id")
	if _, err := uuid.Parse(uploadID); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Invalid upload ID.")
		return
	}

	upload, err := h.uploads.GetUpload(r.Context(), principal.UserID, uploadID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	resp := getResponse{
		UploadID:  upload.ID,
		Filename:  upload.OriginalFilename,
		Content:   upload.ContentType,
		Status:    upload.Status,
		CreatedAt: upload.CreatedAt.UTC().Format(time.RFC3339),
	}
	size := upload.ActualSize
	if size == nil {
		size = upload.ExpectedSize
	}
	resp.SizeBytes = size
	if upload.UploadedAt != nil {
		s := upload.UploadedAt.UTC().Format(time.RFC3339)
		resp.UploadedAt = &s
	}
	writeJSON(w, http.StatusOK, resp)
}

func readRawBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	if r.Body == nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Request body is required.")
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	_ = r.Body.Close()
	if err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return nil, false
	}
	if int64(len(raw)) > limit {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Request body too large.")
		return nil, false
	}
	return raw, true
}

// hashCreateCommand hashes canonical fields so semantically identical
// retries with different JSON whitespace still replay.
func hashCreateCommand(req createRequest) string {
	filename, contentType := domain.NormalizeCreate(req.Filename, req.ContentType)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", filename, contentType, req.SizeBytes)))
	return hex.EncodeToString(sum[:])
}
