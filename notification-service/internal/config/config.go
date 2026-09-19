package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultNtfyURL          = "http://ntfy:80"
	defaultTimeoutSeconds   = 10
	defaultAdvanceSeconds   = 900
	defaultMaxMessageBytes  = 4096
	defaultPollSeconds      = 15
	defaultBatchSize        = 200
	defaultWorkerParallel   = 5
	defaultMaxAttempts      = 5
	defaultReclaimSeconds   = 120
	defaultAdvanceGraceSecs = 120
	defaultMaxLatenessSecs  = 3600
	defaultRetentionDays    = 30
)

// Config is the full notification-service configuration, loaded from
// config.yaml with environment overrides for container/orchestration.
type Config struct {
	Postgres PostgresConfig `yaml:"postgres"`
	Ntfy     NtfyConfig     `yaml:"ntfy"`
	Delivery DeliveryConfig `yaml:"delivery"`
}

type PostgresConfig struct {
	DSNEnv string `yaml:"dsn_env"`
	dsn    string
}

func (p *PostgresConfig) DSN() string { return p.dsn }

// NtfyConfig carries the self-hosted ntfy endpoint and message controls.
type NtfyConfig struct {
	URL             string `yaml:"url"`
	TokenEnv        string `yaml:"token_env"`
	TimeoutSeconds  int    `yaml:"timeout_seconds"`
	AdvanceSeconds  int    `yaml:"advance_seconds"`
	MaxMessageBytes int    `yaml:"max_message_bytes"`
	DuePriority     string `yaml:"due_priority"`
	AdvancePriority string `yaml:"advance_priority"`
	token           string
}

func (n *NtfyConfig) Token() string          { return n.token }
func (n *NtfyConfig) Timeout() time.Duration { return time.Duration(n.TimeoutSeconds) * time.Second }
func (n *NtfyConfig) Advance() time.Duration { return time.Duration(n.AdvanceSeconds) * time.Second }

// DeliveryConfig controls how often and how many reminders are delivered.
type DeliveryConfig struct {
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	BatchSize           int `yaml:"batch_size"`
	WorkerConcurrency   int `yaml:"worker_concurrency"`
	MaxAttempts         int `yaml:"max_attempts"`
	ReclaimAfterSeconds int `yaml:"reclaim_after_seconds"`
	AdvanceGraceSeconds int `yaml:"advance_grace_seconds"`
	MaxLatenessSeconds  int `yaml:"max_lateness_seconds"`
	RetentionDays       int `yaml:"retention_days"`
}

func (d *DeliveryConfig) PollInterval() time.Duration {
	return time.Duration(d.PollIntervalSeconds) * time.Second
}
func (d *DeliveryConfig) ReclaimAfter() time.Duration {
	return time.Duration(d.ReclaimAfterSeconds) * time.Second
}
func (d *DeliveryConfig) AdvanceGrace() time.Duration {
	return time.Duration(d.AdvanceGraceSeconds) * time.Second
}
func (d *DeliveryConfig) MaxLateness() time.Duration {
	return time.Duration(d.MaxLatenessSeconds) * time.Second
}
func (d *DeliveryConfig) Retention() time.Duration {
	return time.Duration(d.RetentionDays) * 24 * time.Hour
}

// Load reads the yaml file at path, resolves *_env fields against the actual
// environment, applies defaults, and rejects invalid values instead of
// silently falling back.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.Postgres.dsn = os.Getenv(cfg.Postgres.DSNEnv)
	if cfg.Postgres.dsn == "" {
		return nil, fmt.Errorf("env var %s is empty", cfg.Postgres.DSNEnv)
	}

	if cfg.Ntfy.URL == "" {
		cfg.Ntfy.URL = defaultNtfyURL
	}
	cfg.Ntfy.token = os.Getenv(cfg.Ntfy.TokenEnv)
	if cfg.Ntfy.token == "" {
		return nil, fmt.Errorf("env var %s is empty", cfg.Ntfy.TokenEnv)
	}
	if cfg.Ntfy.TimeoutSeconds <= 0 {
		cfg.Ntfy.TimeoutSeconds = defaultTimeoutSeconds
	}
	if cfg.Ntfy.AdvanceSeconds <= 0 {
		cfg.Ntfy.AdvanceSeconds = defaultAdvanceSeconds
	}
	if cfg.Ntfy.MaxMessageBytes <= 0 {
		cfg.Ntfy.MaxMessageBytes = defaultMaxMessageBytes
	}

	if cfg.Delivery.PollIntervalSeconds <= 0 {
		cfg.Delivery.PollIntervalSeconds = defaultPollSeconds
	}
	if cfg.Delivery.BatchSize <= 0 {
		cfg.Delivery.BatchSize = defaultBatchSize
	}
	if cfg.Delivery.WorkerConcurrency <= 0 {
		cfg.Delivery.WorkerConcurrency = defaultWorkerParallel
	}
	if cfg.Delivery.MaxAttempts <= 0 {
		cfg.Delivery.MaxAttempts = defaultMaxAttempts
	}
	if cfg.Delivery.ReclaimAfterSeconds <= 0 {
		cfg.Delivery.ReclaimAfterSeconds = defaultReclaimSeconds
	}
	if cfg.Delivery.AdvanceGraceSeconds < 0 {
		return nil, fmt.Errorf("delivery.advance_grace_seconds must be >= 0")
	}
	if cfg.Delivery.AdvanceGraceSeconds == 0 {
		cfg.Delivery.AdvanceGraceSeconds = defaultAdvanceGraceSecs
	}
	if cfg.Delivery.MaxLatenessSeconds <= 0 {
		cfg.Delivery.MaxLatenessSeconds = defaultMaxLatenessSecs
	}
	if cfg.Delivery.RetentionDays <= 0 {
		cfg.Delivery.RetentionDays = defaultRetentionDays
	}
	if cfg.Ntfy.Advance() <= cfg.Delivery.AdvanceGrace() {
		return nil, fmt.Errorf("ntfy.advance_seconds must exceed delivery.advance_grace_seconds")
	}

	return &cfg, nil
}
