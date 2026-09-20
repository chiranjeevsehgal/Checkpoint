// Command api is the ingestion service entrypoint. It wires PostgreSQL,
// MinIO, Kafka and the upload routes, then serves until SIGTERM/SIGINT.
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

	"checkpoint/ingestion/internal/auth"
	"checkpoint/ingestion/internal/cleanup"
	"checkpoint/ingestion/internal/config"
	"checkpoint/ingestion/internal/deletion"
	apihttp "checkpoint/ingestion/internal/http"
	"checkpoint/ingestion/internal/identitycleanup"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/ntfy"
	"checkpoint/ingestion/internal/observability"
	"checkpoint/ingestion/internal/outbox"
	"checkpoint/ingestion/internal/queue"
	"checkpoint/ingestion/internal/repository/postgres"
	"checkpoint/ingestion/internal/service"
	minioimpl "checkpoint/ingestion/internal/storage/minio"
)

func main() {
	logger := observability.New("ingestion-api")
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
	requestPool, err := postgres.NewPool(ctx, cfg.DatabaseRequestURL)
	if err != nil {
		logger.Error("postgres request pool connect failed", "error", err)
		os.Exit(1)
	}
	workerPool, err := postgres.NewPool(ctx, cfg.DatabaseWorkerURL)
	if err != nil {
		logger.Error("postgres worker pool connect failed", "error", err)
		os.Exit(1)
	}

	objectStorage, err := minioimpl.New(ctx,
		cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOBucket, cfg.MinIOUseSSL, cfg.MinIOPublicEndpoint)
	if err != nil {
		logger.Error("minio connect failed", "error", err)
		os.Exit(1)
	}

	var kratosAdmin *auth.KratosAdmin
	if cfg.KratosAdminURL != "" {
		kratosAdmin = auth.NewKratosAdmin(cfg.KratosAdminURL, cfg.KratosTimeout)
	}

	authenticator := auth.NewKratosAuthenticator(cfg.KratosPublicURL, cfg.KratosTimeout)
	uploads := service.NewUploadService(requestPool, requestPool, objectStorage, cfg.MinIOBucket, nil)
	devices := service.NewDeviceService(requestPool)
	accounts := service.NewAccountService(requestPool)
	settings := service.NewSettingsService(requestPool)
	ntfyAdmin := newNtfyAdmin(cfg)
	notifications := service.NewNotificationService(requestPool, notificationProvisioner(ntfyAdmin))
	mcpKeys := service.NewMcpKeyService(requestPool)
	reg := metrics.NewRegistry()
	deps := apihttp.RouterDeps{
		Auth:          authenticator,
		Accounts:      requestPool,
		Uploads:       uploads,
		Devices:       devices,
		Account:       accounts,
		Settings:      settings,
		Notifications: notifications,
		McpKeys:       mcpKeys,
		NTFPPublicURL: cfg.NTFPPublicURL,
		Idem:          requestPool,
		DB:            requestPool,
		Storage:       objectStorage,
		Metrics:       reg,
	}
	if kratosAdmin != nil {
		deps.Sessions = kratosAdmin
	}
	mux := apihttp.NewRouter(deps)

	// The outbox dispatcher runs in-process. Every replica runs one, and
	// SKIP LOCKED claiming keeps them from stepping on each other.
	// Published records point workers at MinIO objects for Deepgram.
	publisher, err := queue.NewFranzProducer(cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaClientID+"-"+instanceID())
	if err != nil {
		logger.Error("kafka connect failed", "error", err)
		os.Exit(1)
	}
	dispatcher := outbox.NewDispatcher(workerPool, publisher, instanceID(), logger, reg)

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

	var identityDeleter auth.IdentityDeleter
	if kratosAdmin != nil {
		identityDeleter = kratosAdmin
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	workers := 3
	if kratosAdmin != nil {
		workers++
	}
	wg.Add(workers)
	go func() { defer wg.Done(); dispatcher.Run(runCtx) }()
	cleaner := cleanup.NewCleaner(workerPool, objectStorage, cfg.UploadExpiry, cfg.CleanupInterval, logger)
	go func() { defer wg.Done(); cleaner.Run(runCtx) }()
	deletionWorker := deletion.NewWorker(workerPool, objectStorage, identityDeleter, ntfyUserDeleter(ntfyAdmin), logger)
	go func() { defer wg.Done(); deletionWorker.Run(runCtx) }()
	if kratosAdmin != nil {
		reaper := identitycleanup.NewWorker(kratosAdmin, kratosAdmin, cfg.IdentityTTL, cfg.IdentityCleanupInterval, logger)
		go func() { defer wg.Done(); reaper.Run(runCtx) }()
	}

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
	publisher.Close()
	requestPool.Close()
	workerPool.Close()
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

// newNtfyAdmin returns a provisioner client, or nil when per-user ntfy auth is
// not configured (the anonymous read-only fallback).
func newNtfyAdmin(cfg config.Config) *ntfy.AdminClient {
	if cfg.NTFYAdminToken == "" {
		return nil
	}
	return ntfy.NewAdmin(ntfy.AdminOptions{
		BaseURL: cfg.NTFYAdminURL,
		Token:   cfg.NTFYAdminToken,
		Timeout: cfg.NTFYTimeout,
	})
}

// notificationProvisioner collapses a nil admin client to a nil interface so
// the service's fallback check works.
func notificationProvisioner(admin *ntfy.AdminClient) service.NotificationProvisioner {
	if admin == nil {
		return nil
	}
	return admin
}

// ntfyUserDeleter does the same for the account-deletion worker.
func ntfyUserDeleter(admin *ntfy.AdminClient) deletion.NtfyUserDeleter {
	if admin == nil {
		return nil
	}
	return admin
}
