package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
)

// Pinger reports dependency reachability for the readiness probe.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Router wires health and upload routes with middleware.
type Router struct {
	handler  *Handler
	devices  *DeviceHandler
	account  *AccountHandler
	sessions *SessionHandler
	db       Pinger
	storage  Pinger
	reg      *metrics.Registry
}

// RouterDeps carries the router's collaborators.
type RouterDeps struct {
	Auth     Authenticator
	Accounts AccountGuard
	Uploads  uploadService
	Devices  deviceService
	Account  accountService
	Sessions sessionRevoker
	Idem     repository.IdempotencyRepository
	DB       Pinger
	Storage  Pinger
	Metrics  *metrics.Registry
}

// NewRouter builds the full route tree. Health and metrics endpoints
// stay outside Auth; everything under /v1 requires it.
func NewRouter(deps RouterDeps) http.Handler {
	r := &Router{
		handler:  NewHandler(deps.Uploads, deps.Devices, deps.Idem, deps.Metrics),
		devices:  NewDeviceHandler(deps.Devices, deps.Metrics),
		account:  NewAccountHandler(deps.Account, deps.Metrics),
		sessions: NewSessionHandler(deps.Sessions),
		db:       deps.DB,
		storage:  deps.Storage,
		reg:      deps.Metrics,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", live)
	mux.HandleFunc("GET /health/ready", r.ready)
	mux.HandleFunc("GET /metrics", r.serveMetrics)

	protected := func(h http.HandlerFunc) http.Handler {
		return RequestID(Observe(deps.Metrics, Auth(deps.Auth, deps.Accounts, h)))
	}
	mux.Handle("POST /v1/uploads", protected(r.handler.CreateUpload))
	mux.Handle("POST /v1/uploads/{id}/complete", protected(r.handler.CompleteUpload))
	mux.Handle("GET /v1/uploads/{id}", protected(r.handler.GetUpload))
	mux.Handle("GET /v1/device", protected(r.devices.Get))
	mux.Handle("POST /v1/device/claim", protected(r.devices.Claim))
	mux.Handle("POST /v1/device/release", protected(r.devices.Release))
	mux.Handle("DELETE /v1/me", protected(r.account.Delete))
	mux.Handle("DELETE /v1/me/sessions", protected(r.sessions.Delete))
	return mux
}

func live(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// serveMetrics renders the Prometheus exposition. No auth: scrape from
// the private network only.
func (r *Router) serveMetrics(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder
	r.reg.Write(&b)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(b.String()))
}

// ready checks PostgreSQL and object storage only. The transcription
// queue is deliberately excluded so broker downtime never takes ingestion
// out of service.
func (r *Router) ready(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
	defer cancel()

	if r.db != nil {
		if err := r.db.Ping(ctx); err != nil {
			writeError(w, req, http.StatusServiceUnavailable, CodeInternal, "Database is not ready.")
			return
		}
	}
	if r.storage != nil {
		if err := r.storage.Ping(ctx); err != nil {
			writeError(w, req, http.StatusServiceUnavailable, CodeInternal, "Object storage is not ready.")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
