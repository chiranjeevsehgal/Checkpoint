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
	handler *Handler
	db      Pinger
	storage Pinger
	reg     *metrics.Registry
}

// NewRouter builds the full route tree. Health and metrics endpoints
// stay outside Auth; everything under /v1 requires it.
func NewRouter(uploads uploadService, idem repository.IdempotencyRepository, db, objectStore Pinger, reg *metrics.Registry) http.Handler {
	r := &Router{handler: NewHandler(uploads, idem, reg), db: db, storage: objectStore, reg: reg}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", live)
	mux.HandleFunc("GET /health/ready", r.ready)
	mux.HandleFunc("GET /metrics", r.serveMetrics)

	protected := func(h http.HandlerFunc) http.Handler {
		return RequestID(Observe(reg, Auth(h)))
	}
	mux.Handle("POST /v1/uploads", protected(r.handler.CreateUpload))
	mux.Handle("POST /v1/uploads/{id}/complete", protected(r.handler.CompleteUpload))
	mux.Handle("GET /v1/uploads/{id}", protected(r.handler.GetUpload))
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

// ready checks PostgreSQL and object storage only. VAD is deliberately
// excluded so VAD downtime never takes ingestion out of service.
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
