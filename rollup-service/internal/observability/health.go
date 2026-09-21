// Health endpoints for workers: liveness, readiness and a dependency-free
// Prometheus counter exposition. Mirrors ingestion-service's /metrics shape
// (same text format, no client library) so the shared Prometheus job works.
// Each Go module is independent, so this file is intentionally duplicated
// across the worker services instead of shared.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HealthServer counts process events and serves them to Prometheus. Ready
// means the worker finished startup and entered its loop; dependencies were
// validated at startup (the process exits otherwise), so no per-request
// probing happens here.
type HealthServer struct {
	addr   string
	ready  atomic.Bool
	mu     sync.Mutex
	counts map[string]int64
}

// NewHealthServer builds the server. METRICS_ADDR overrides addr.
func NewHealthServer(addr string) *HealthServer {
	if env := os.Getenv("METRICS_ADDR"); env != "" {
		addr = env
	}
	return &HealthServer{addr: addr, counts: map[string]int64{}}
}

// SetReady flips the readiness probe after startup completes.
func (s *HealthServer) SetReady(ready bool) {
	s.ready.Store(ready)
}

// Inc adds one to the named counter.
func (s *HealthServer) Inc(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[name]++
}

// Start serves until ctx ends. It never fails the process: a bind error is
// logged and the worker keeps running without metrics.
func (s *HealthServer) Start(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !s.ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"starting"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		var b strings.Builder
		for _, name := range s.names() {
			fmt.Fprintf(&b, "# TYPE %s counter\n%s %d\n", name, name, s.get(name))
		}
		_, _ = w.Write([]byte(b.String()))
	})
	server := &http.Server{Addr: s.addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server failed; continuing without metrics", "addr", s.addr, "error", err)
		}
	}()
}

func (s *HealthServer) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.counts))
	for name := range s.counts {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *HealthServer) get(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[name]
}
