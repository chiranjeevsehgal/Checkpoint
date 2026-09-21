// Package config loads service configuration from the environment and
// from ingestion-service/config.yaml (single source of truth for shared
// queue names). Load fails fast if the YAML is missing or values drift.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v3"
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

	// DatabaseURL is the privileged PostgreSQL connection string used by
	// migrations. DatabaseRequestURL is the NOBYPASSRLS request role and
	// DatabaseWorkerURL the trusted background role; both default to
	// DatabaseURL in development.
	DatabaseURL        string
	DatabaseRequestURL string
	DatabaseWorkerURL  string

	// KratosPublicURL validates opaque session tokens via /sessions/whoami.
	KratosPublicURL string
	// KratosAdminURL is the private admin API used by the deletion worker.
	KratosAdminURL string
	KratosTimeout  time.Duration

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

	// IdentityTTL is how old an unverified identity must be before the
	// cleanup worker deletes it. IdentityCleanupInterval is the sweep period.
	IdentityTTL             time.Duration
	IdentityCleanupInterval time.Duration

	// NTFPPublicURL is the base URL clients use to subscribe to ntfy, returned
	// by GET/POST /v1/me/notifications. It must be reachable from the phone.
	NTFPPublicURL string
	// NTFYAdminURL/NTFYAdminToken enable per-user ntfy read authorization via
	// the ntfy admin API. An empty token keeps the anonymous read-only fallback.
	NTFYAdminURL   string
	NTFYAdminToken string
	NTFYTimeout    time.Duration

	// HTTP timeouts, see LLD section 22.
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration

	// RateLimitRPS/Burst bound per-user request rate (RATE_LIMIT_RPS,
	// RATE_LIMIT_BURST). Non-positive values disable limiting.
	RateLimitRPS   int
	RateLimitBurst int

	// PoolMaxConns/MinConns size the Postgres pools (POOL_MAX_CONNS,
	// POOL_MIN_CONNS).
	PoolMaxConns int
	PoolMinConns int
}

// Load reads configuration from the environment. Development keeps
// lenient defaults; production requires explicit secrets and endpoints
// and rejects invalid values instead of silently falling back.
// The Kafka transcription topic comes from config.yaml (single source of
// truth): KAFKA_TOPIC_TRANSCRIPTION must be unset or exactly match it,
// otherwise Load fails.
func Load() (Config, error) {
	canonicalTopic, err := loadCanonicalTopic()
	if err != nil {
		return Config{}, err
	}
	if v := strings.TrimSpace(os.Getenv("KAFKA_TOPIC_TRANSCRIPTION")); v != "" && v != canonicalTopic {
		return Config{}, fmt.Errorf("KAFKA_TOPIC_TRANSCRIPTION %q does not match config.yaml %q: single source of truth is ingestion-service/config.yaml", v, canonicalTopic)
	}
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
	identityHours, err := parsePositiveIntStrict("IDENTITY_TTL_HOURS", 1)
	if err != nil {
		return Config{}, err
	}
	identityCleanupMinutes, err := parsePositiveIntStrict("IDENTITY_CLEANUP_INTERVAL_MINUTES", 15)
	if err != nil {
		return Config{}, err
	}
	kratosSeconds, err := parsePositiveIntStrict("KRATOS_TIMEOUT_SECONDS", 5)
	if err != nil {
		return Config{}, err
	}
	ntfySeconds, err := parsePositiveIntStrict("NTFY_TIMEOUT_SECONDS", 10)
	if err != nil {
		return Config{}, err
	}
	rateRPS, err := parsePositiveIntStrict("RATE_LIMIT_RPS", 10)
	if err != nil {
		return Config{}, err
	}
	rateBurst, err := parsePositiveIntStrict("RATE_LIMIT_BURST", 20)
	if err != nil {
		return Config{}, err
	}
	poolMax, err := parsePositiveIntStrict("POOL_MAX_CONNS", 20)
	if err != nil {
		return Config{}, err
	}
	poolMin, err := parsePositiveIntStrict("POOL_MIN_CONNS", 5)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:                    envOr("PORT", defaultPort),
		Env:                     envOr("ENV", defaultEnv),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		DatabaseRequestURL:      envOr("DATABASE_REQUEST_URL", os.Getenv("DATABASE_URL")),
		DatabaseWorkerURL:       envOr("DATABASE_WORKER_URL", os.Getenv("DATABASE_URL")),
		KratosPublicURL:         strings.TrimRight(os.Getenv("KRATOS_PUBLIC_URL"), "/"),
		KratosAdminURL:          strings.TrimRight(os.Getenv("KRATOS_ADMIN_URL"), "/"),
		KratosTimeout:           time.Duration(kratosSeconds) * time.Second,
		MinIOEndpoint:           envOr("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:          os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey:          os.Getenv("MINIO_SECRET_KEY"),
		MinIOUseSSL:             useSSL,
		MinIOBucket:             envOr("MINIO_BUCKET", "audio"),
		MinIOPublicEndpoint:     os.Getenv("MINIO_PUBLIC_ENDPOINT"),
		KafkaBrokers:            envOr("KAFKA_BROKERS", "kafka:9092"),
		KafkaTopic:              canonicalTopic,
		KafkaClientID:           envOr("KAFKA_CLIENT_ID", "ingestion"),
		UploadExpiry:            time.Duration(uploadHours) * time.Hour,
		CleanupInterval:         time.Duration(cleanupMinutes) * time.Minute,
		IdentityTTL:             time.Duration(identityHours) * time.Hour,
		IdentityCleanupInterval: time.Duration(identityCleanupMinutes) * time.Minute,
		NTFPPublicURL:           strings.TrimRight(envOr("NTFY_PUBLIC_URL", "http://localhost:8085"), "/"),
		NTFYAdminURL:            strings.TrimRight(envOr("NTFY_ADMIN_URL", "http://ntfy:80"), "/"),
		NTFYAdminToken:          strings.TrimSpace(os.Getenv("NTFY_ADMIN_TOKEN")),
		NTFYTimeout:             time.Duration(ntfySeconds) * time.Second,
		ReadHeaderTimeout:       5 * time.Second,
		ReadTimeout:             15 * time.Second,
		WriteTimeout:            15 * time.Second,
		IdleTimeout:             60 * time.Second,
		ShutdownTimeout:         defaultShutdownTimeout,
		RateLimitRPS:            rateRPS,
		RateLimitBurst:          rateBurst,
		PoolMaxConns:            poolMax,
		PoolMinConns:            poolMin,
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
		if strings.TrimSpace(os.Getenv("DATABASE_REQUEST_URL")) == "" {
			return Config{}, fmt.Errorf("DATABASE_REQUEST_URL must be set in production")
		}
		if strings.TrimSpace(os.Getenv("DATABASE_WORKER_URL")) == "" {
			return Config{}, fmt.Errorf("DATABASE_WORKER_URL must be set in production")
		}
		if cfg.KratosPublicURL == "" {
			return Config{}, fmt.Errorf("KRATOS_PUBLIC_URL must be set in production")
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
		if strings.TrimSpace(os.Getenv("NTFY_PUBLIC_URL")) == "" {
			return Config{}, fmt.Errorf("NTFY_PUBLIC_URL must be set in production")
		}
		// KAFKA_TOPIC_TRANSCRIPTION intentionally not required here: the
		// canonical value comes from config.yaml and env mismatch already
		// fails above in all environments.
	}

	return cfg, nil
}

// fileConfig mirrors ingestion-service/config.yaml (single source of truth).
type fileConfig struct {
	Kafka struct {
		TopicTranscription string `yaml:"topic_transcription"`
	} `yaml:"kafka"`
}

// loadCanonicalTopic reads the transcription topic from config.yaml.
// CONFIG_FILE overrides the path. Otherwise search: ./config.yaml (go run
// from ingestion-service), ../../config.yaml (go test from internal/config),
// /config.yaml (distroless image). Missing files are skipped; a file that
// exists but fails to parse or validate fails fast instead of silently
// falling through to a different config.yaml.
func loadCanonicalTopic() (string, error) {
	if p := strings.TrimSpace(os.Getenv("CONFIG_FILE")); p != "" {
		return readTopicFile(p)
	}
	candidates := []string{"config.yaml", filepath.Join("..", "..", "config.yaml"), filepath.Join(string(filepath.Separator), "config.yaml")}
	// Also try relative to the working directory's parent (repo root layouts).
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "config.yaml"),
			filepath.Join(wd, "..", "..", "config.yaml"),
			filepath.Join(wd, "ingestion-service", "config.yaml"),
		)
	}
	var lastErr error
	for _, p := range candidates {
		topic, err := readTopicFile(p)
		if err == nil {
			return topic, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		lastErr = err
	}
	return "", fmt.Errorf("load config.yaml (single source of truth for kafka topic): %v", lastErr)
}

func readTopicFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var fc fileConfig
	if err := yaml.Unmarshal(raw, &fc); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	topic := strings.TrimSpace(fc.Kafka.TopicTranscription)
	if topic == "" {
		return "", fmt.Errorf("parse %s: kafka.topic_transcription must not be empty", path)
	}
	if err := validateTopicName(path, topic); err != nil {
		return "", err
	}
	return topic, nil
}

func validateTopicName(path, topic string) error {
	if len(topic) > 249 {
		return fmt.Errorf("parse %s: kafka.topic_transcription %q too long (max 249)", path, topic)
	}
	for _, r := range topic {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("parse %s: kafka.topic_transcription %q contains invalid character %q (allowed: A-Z a-z 0-9 . _ -)", path, topic, string(r))
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
