// Package config loads service configuration from the environment.
//
// Only stdlib is used here so the scaffold builds without external
// dependencies.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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

	// Kafka settings for the transcription queue.
	KafkaBrokers  string
	KafkaTopic    string
	KafkaClientID string

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

// Load reads configuration from the environment. Development keeps
// lenient defaults; production requires explicit secrets and endpoints
// and rejects invalid values instead of silently falling back.
func Load() (Config, error) {
	useSSL, err := parseBoolStrict("MINIO_USE_SSL", false)
	if err != nil {
		return Config{}, err
	}
	uploadHours, err := parsePositiveIntStrict("UPLOAD_EXPIRY_HOURS", 24)
	if err != nil {
		return Config{}, err
	}
	cleanupMinutes, err := parsePositiveIntStrict("CLEANUP_INTERVAL_MINUTES", 30)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:                envOr("PORT", defaultPort),
		Env:                 envOr("ENV", defaultEnv),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		MinIOEndpoint:       envOr("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:      os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey:      os.Getenv("MINIO_SECRET_KEY"),
		MinIOUseSSL:         useSSL,
		MinIOBucket:         envOr("MINIO_BUCKET", "audio"),
		MinIOPublicEndpoint: os.Getenv("MINIO_PUBLIC_ENDPOINT"),
		KafkaBrokers:        envOr("KAFKA_BROKERS", "kafka:9092"),
		KafkaTopic:          envOr("KAFKA_TOPIC_TRANSCRIPTION", "transcription.jobs.v1"),
		KafkaClientID:       envOr("KAFKA_CLIENT_ID", "ingestion"),
		UploadExpiry:        time.Duration(uploadHours) * time.Hour,
		CleanupInterval:     time.Duration(cleanupMinutes) * time.Minute,
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
	if n, err := strconv.Atoi(cfg.Port); err != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("invalid PORT %q: must be 1-65535", cfg.Port)
	}
	if cfg.MinIOPublicEndpoint == "" {
		cfg.MinIOPublicEndpoint = cfg.MinIOEndpoint
	}

	if strings.EqualFold(cfg.Env, "production") {
		if cfg.DatabaseURL == "" {
			return Config{}, fmt.Errorf("DATABASE_URL must be set in production")
		}
		if cfg.MinIOAccessKey == "" || cfg.MinIOSecretKey == "" {
			return Config{}, fmt.Errorf("MINIO_ACCESS_KEY and MINIO_SECRET_KEY must be set in production")
		}
		if cfg.MinIOBucket == "" {
			return Config{}, fmt.Errorf("MINIO_BUCKET must be set in production")
		}
		if strings.TrimSpace(os.Getenv("KAFKA_BROKERS")) == "" {
			return Config{}, fmt.Errorf("KAFKA_BROKERS must be set in production")
		}
		if strings.TrimSpace(os.Getenv("KAFKA_TOPIC_TRANSCRIPTION")) == "" {
			return Config{}, fmt.Errorf("KAFKA_TOPIC_TRANSCRIPTION must be set in production")
		}
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
	v, err := parseBoolStrict(key, fallback)
	if err != nil {
		return fallback
	}
	return v
}

func parseBoolStrict(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: must be true/false", key, v)
	}
	return b, nil
}

func envDuration(key string, fallback int) time.Duration {
	v, err := parsePositiveIntStrict(key, fallback)
	if err != nil {
		return time.Duration(fallback)
	}
	return time.Duration(v)
}

func parsePositiveIntStrict(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be a positive integer", key, v)
	}
	return n, nil
}
