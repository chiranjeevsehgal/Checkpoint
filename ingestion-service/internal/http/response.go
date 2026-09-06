package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/service"
	"checkpoint/ingestion/internal/storage"
)

// Error codes from the LLD error model.
const (
	CodeInvalidRequest       = "INVALID_REQUEST"
	CodeUnauthorized         = "UNAUTHORIZED"
	CodeUploadNotFound       = "UPLOAD_NOT_FOUND"
	CodeInvalidState         = "INVALID_UPLOAD_STATE"
	CodeObjectNotFound       = "OBJECT_NOT_FOUND"
	CodeSizeMismatch         = "SIZE_MISMATCH"
	CodeUploadTooLarge       = "UPLOAD_TOO_LARGE"
	CodeUnsupportedMediaType = "UNSUPPORTED_MEDIA_TYPE"
	CodeInternal             = "INTERNAL_ERROR"
)

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// writeJSON encodes a success payload.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError encodes the single API error representation.
func writeError(w http.ResponseWriter, r *http.Request, code int, errCode, message string) {
	writeJSON(w, code, errorEnvelope{Error: errorBody{
		Code:      errCode,
		Message:   message,
		RequestID: RequestIDFrom(r.Context()),
	}})
}

// writeServiceError maps domain, service and infrastructure errors to
// the LLD status-code table.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, CodeUploadNotFound, "Upload was not found.")
	case errors.Is(err, repository.ErrInvalidState):
		writeError(w, r, http.StatusConflict, CodeInvalidState, "Upload is not in a state for this operation.")
	case errors.Is(err, storage.ErrObjectNotFound):
		writeError(w, r, http.StatusConflict, CodeObjectNotFound, "No uploaded object found. Upload the file first.")
	case errors.Is(err, service.ErrSizeMismatch):
		writeError(w, r, http.StatusConflict, CodeSizeMismatch, "Uploaded object size does not match.")
	case errors.Is(err, domain.ErrTooLarge):
		writeError(w, r, http.StatusRequestEntityTooLarge, CodeUploadTooLarge, "Upload exceeds the maximum size.")
	case errors.Is(err, domain.ErrUnsupportedMediaType):
		writeError(w, r, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Content type is not supported.")
	case errors.Is(err, domain.ErrInvalidFilename),
		errors.Is(err, domain.ErrInvalidSize):
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, CodeInternal, "Unexpected internal error.")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Request body is required.")
		return false
	}
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed JSON request body.")
		return false
	}
	return true
}
