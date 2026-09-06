// Command api is the ingestion service entrypoint. It wires PostgreSQL,
// MinIO and the upload routes, then serves until SIGTERM/SIGINT. The
// outbox dispatcher and cleanup loop are added by later tasks on top of
// the lifecycle defined here.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"checkpoint/ingestion/internal/cleanup"
	"checkpoint/ingestion/internal/config"
	apihttp "checkpoint/ingestion/internal/http"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/outbox"
	"checkpoint/ingestion/internal/repository/postgres"
	"checkpoint/ingestion/internal/service"
	minioimpl "checkpoint/ingestion/internal/storage/minio"
	"checkpoint/ingestion/internal/vadclient"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "error", err)
		os.Exit(1)
	}
	if cfg.DatabaseURL == "" {
		logger.Error("DATABASE_URL must be set")
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres connect failed", "error", err)
		os.Exit(1)
	}

	objectStorage, err := minioimpl.New(ctx,
		cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOBucket, cfg.MinIOUseSSL, cfg.MinIOPublicEndpoint)
	if err != nil {
		logger.Error("minio connect failed", "error", err)
		os.Exit(1)
	}

	uploads := service.NewUploadService(pool, pool, objectStorage, cfg.MinIOBucket, nil)
	reg := metrics.NewRegistry()
	mux := apihttp.NewRouter(uploads, pool, pool, objectStorage, reg)

	// The outbox dispatcher runs in-process. Every replica runs one, and
	// SKIP LOCKED claiming keeps them from stepping on each other.
	dispatcher := outbox.NewDispatcher(pool, vadclient.New(cfg.VADBaseURL), instanceID(), logger, reg)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runCtx, cancelRun := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); dispatcher.Run(runCtx) }()
	cleaner := cleanup.NewCleaner(pool, objectStorage, cfg.UploadExpiry, cfg.CleanupInterval, logger)
	go func() { defer wg.Done(); cleaner.Run(runCtx) }()

	go func() {
		logger.Info("ingestion api listening", "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	logger.Info("shutdown signal received, draining")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	cancelRun()
	wg.Wait()
	pool.Close()
	logger.Info("shutdown complete")
}

// instanceID uniquely names this replica for outbox lease ownership.
func instanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
