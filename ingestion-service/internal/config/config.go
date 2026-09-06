// Package config loads service configuration from the environment.
//
// Only stdlib is used here so the scaffold builds without external
// dependencies. Later tasks add DATABASE_URL, MinIO and VAD settings
// consumers; the field names are fixed now to avoid rework.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultPort            = "8080"
	defaultEnv             = "development"
	defaultShutdownTimeout = 30 * time.Second
)

// Config holds all runtime settings for the ingestion API process.
type Config struct {
	Port string
	Env  string

	// DatabaseURL is the PostgreSQL connection string.
	// Consumed from task 2 (migrations) onwards.
	DatabaseURL string

	// MinIO settings, consumed from task 5 onwards.
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOUseSSL    bool
	MinIOBucket    string
	// MinIOPublicEndpoint is the host clients use to reach MinIO. Inside
	// Docker the API talks to MinIO over the internal endpoint, but the
	// presigned URL must point at an address the client can resolve.
	// Defaults to MinIOEndpoint when unset.
	MinIOPublicEndpoint string

	// VADBaseURL is the internal VAD job endpoint, consumed from task 8.
	VADBaseURL string

	// UploadExpiry is how long an UPLOADING row may sit untouched before
	// the cleanup job marks it EXPIRED.
	UploadExpiry time.Duration
	// CleanupInterval is how often the expiry sweep runs.
	CleanupInterval time.Duration

	// HTTP timeouts, see LLD section 22.
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		Port:                envOr("PORT", defaultPort),
		Env:                 envOr("ENV", defaultEnv),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		MinIOEndpoint:       envOr("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:      os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey:      os.Getenv("MINIO_SECRET_KEY"),
		MinIOUseSSL:         envBool("MINIO_USE_SSL", false),
		MinIOBucket:         envOr("MINIO_BUCKET", "audio"),
		MinIOPublicEndpoint: os.Getenv("MINIO_PUBLIC_ENDPOINT"),
		VADBaseURL:          envOr("VAD_BASE_URL", "http://localhost:8081"),
		UploadExpiry:        envDuration("UPLOAD_EXPIRY_HOURS", 24) * time.Hour,
		CleanupInterval:     envDuration("CLEANUP_INTERVAL_MINUTES", 30) * time.Minute,
		ReadHeaderTimeout:   5 * time.Second,
		ReadTimeout:         15 * time.Second,
		WriteTimeout:        15 * time.Second,
		IdleTimeout:         60 * time.Second,
		ShutdownTimeout:     defaultShutdownTimeout,
	}

	if v := os.Getenv("SHUTDOWN_TIMEOUT_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("invalid SHUTDOWN_TIMEOUT_SECONDS %q: must be a positive integer", v)
		}
		cfg.ShutdownTimeout = time.Duration(n) * time.Second
	}

	if cfg.Port == "" {
		return Config{}, fmt.Errorf("PORT must not be empty")
	}
	if cfg.MinIOPublicEndpoint == "" {
		cfg.MinIOPublicEndpoint = cfg.MinIOEndpoint
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envDuration(key string, fallback int) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(fallback)
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return time.Duration(fallback)
	}
	return time.Duration(n)
}
