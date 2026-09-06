// Command mockvad is a local-development stand-in for the VAD service.
// It accepts job submissions, keeps them in memory, and exposes them for
// end-to-end assertions. It is not part of the production deployment.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
)

type store struct {
	mu   sync.Mutex
	jobs []json.RawMessage
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	s := &store{}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.jobs = append(s.jobs, raw)
		n := len(s.jobs)
		s.mu.Unlock()
		logger.Info("job accepted", "total", n)
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /received", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.jobs)
	})
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	logger.Info("mock vad listening", "port", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		logger.Error("mock vad failed", "error", err)
		os.Exit(1)
	}
}
