package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"checkpoint/ingestion/internal/auth"
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

// Principal is the authenticated caller, sourced only from the identity
// provider.
type Principal = auth.Principal

// RecentAuthWindow is how fresh a session must be for destructive actions.
const RecentAuthWindow = 5 * time.Minute

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
	if len(parts) >= 2 && parts[0] == "v1" && parts[1] == "me" {
		if len(parts) == 3 && parts[2] == "sessions" {
			return "/v1/me/sessions"
		}
		if len(parts) == 3 && parts[2] == "settings" {
			return "/v1/me/settings"
		}
		return "/v1/me"
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

// Authenticator validates a bearer session token.
type Authenticator interface {
	Authenticate(ctx context.Context, sessionToken string) (Principal, error)
}

// AccountGuard reports whether an identity is being deleted.
type AccountGuard interface {
	IsDeleting(ctx context.Context, userID string) (bool, error)
}

// Auth validates the bearer session against the identity provider and
// attaches the resulting Principal. A provider outage is a 503, never a 401.
func Auth(authenticator Authenticator, guard AccountGuard, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
			return
		}
		principal, err := authenticator.Authenticate(r.Context(), token)
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		if guard != nil {
			deleting, err := guard.IsDeleting(r.Context(), principal.UserID)
			if err != nil {
				writeError(w, r, http.StatusServiceUnavailable, CodeAuthUnavailable, "Authentication is temporarily unavailable.")
				return
			}
			if deleting {
				writeError(w, r, http.StatusForbidden, CodeAccountDeleting, "This account is being deleted.")
				return
			}
		}
		ctx := context.WithValue(r.Context(), principalKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidSession):
		writeError(w, r, http.StatusUnauthorized, CodeUnauthorized, "Missing or invalid authorization.")
	case errors.Is(err, auth.ErrVerificationRequired):
		writeError(w, r, http.StatusForbidden, CodeVerificationRequired, "Email verification is required.")
	default:
		writeError(w, r, http.StatusServiceUnavailable, CodeAuthUnavailable, "Authentication is temporarily unavailable.")
	}
}

// HasRecentAuth reports whether the session was re-authenticated recently.
func HasRecentAuth(principal Principal, now time.Time) bool {
	return !principal.AuthenticatedAt.IsZero() && now.Sub(principal.AuthenticatedAt) <= RecentAuthWindow
}

// HasRecentEmailVerification reports whether the caller verified their email
// recently, which acts as a step-up for destructive account actions.
func HasRecentEmailVerification(principal Principal, now time.Time) bool {
	return !principal.EmailVerifiedAt.IsZero() && now.Sub(principal.EmailVerifiedAt) <= RecentAuthWindow
}
