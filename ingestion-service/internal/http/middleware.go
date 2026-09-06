package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"checkpoint/ingestion/internal/metrics"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// RequestIDFrom returns the request ID carried by the context, if any.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// RequestID assigns every request an ID for log correlation and the
// error envelope. A client-provided X-Request-ID is honored when present
// after sanitizing to prevent log/header injection and unbounded values.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = "req_" + uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func sanitizeRequestID(v string) string {
	if len(v) > 128 {
		v = v[:128]
	}
	if v == "" {
		return ""
	}
	for _, c := range v {
		if c < 32 || c == 127 {
			return ""
		}
	}
	return v
}

// Principal is the authenticated caller. TenantID is reserved for the
// future multi-tenant identity integration.
type Principal struct {
	UserID string
}

// PrincipalFrom returns the caller attached by Auth.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

type principalKey struct{}

// normalizeRoute maps request paths to bounded route templates so metric
// labels cannot explode with upload IDs.
func normalizeRoute(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && parts[0] == "health" {
		return "/health/" + parts[1]
	}
	if path == "/metrics" {
		return "/metrics"
	}
	if len(parts) >= 2 && parts[0] == "v1" && parts[1] == "uploads" {
		switch {
		case len(parts) == 2:
			return "/v1/uploads"
		case len(parts) == 3:
			return "/v1/uploads/{id}"
		case len(parts) == 4 && parts[3] == "complete":
			return "/v1/uploads/{id}/complete"
		}
	}
	return "other"
}

// statusWriter captures the response code for logs and metrics.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// Observe logs each request and records HTTP metrics. Route templates
// keep label cardinality bounded instead of raw IDs.
func Observe(reg *metrics.Registry, next http.Handler) http.Handler {
	requests := reg.Counter("http_requests_total", "method", "route", "code")
	latency := reg.Histogram("http_request_duration_seconds", metrics.DefaultLatencyBuckets, "route")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := normalizeRoute(r.URL.Path)
		code := strconv.Itoa(sw.code)
		requests.Inc(r.Method, route, code)
		latency.Observe(time.Since(start).Seconds(), route)
		slog.Info("request",
			"request_id", RequestIDFrom(r.Context()),
			"method", r.Method, "route", route, "status", sw.code,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// Auth enforces Authorization on protected routes.
//
// TODO(auth): replace the dev stand-in with real JWT validation against
// the identity system and populate TenantID. For V1 the bearer token is
// the user's UUID, which keeps the ownership contract testable without
// an IdP.
func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
			return
		}
		userID := header[len(prefix):]
		if _, err := uuid.Parse(userID); err != nil {
			writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, Principal{UserID: userID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
