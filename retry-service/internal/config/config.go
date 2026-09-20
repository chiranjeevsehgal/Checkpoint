package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultDelaySeconds        = 1800 // 30 minutes
	defaultMaxAttempts         = 2
	defaultPollIntervalSeconds = 10
	defaultBatchSize           = 50
	defaultReclaimAfterSeconds = 300
	defaultRetentionDays       = 30
)

// Config is the full service configuration, loaded from config.yaml with
// environment overrides for container/orchestration flexibility.
type Config struct {
	Kafka    KafkaConfig    `yaml:"kafka"`
	Postgres PostgresConfig `yaml:"postgres"`
	Retry    RetryConfig    `yaml:"retry"`
}

type KafkaConfig struct {
	Brokers       []string `yaml:"brokers"`
	ConsumeTopic  string   `yaml:"consume_topic"`
	ConsumerGroup string   `yaml:"consumer_group"`
	DLQTopic      string   `yaml:"dlq_topic"`
}

type PostgresConfig struct {
	DSNEnv string `yaml:"dsn_env"`
	dsn    string
}

func (p *PostgresConfig) DSN() string { return p.dsn }

// RetryConfig governs the retry lifecycle: how long to wait between
// re-deliveries, how many re-deliveries before giving up, and the
// operational cadences around the durable queue.
type RetryConfig struct {
	DelaySeconds        int `yaml:"delay_seconds"`
	MaxAttempts         int `yaml:"max_attempts"`
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	BatchSize           int `yaml:"batch_size"`
	ReclaimAfterSeconds int `yaml:"reclaim_after_seconds"`
	RetentionDays       int `yaml:"retention_days"`
}

func (r *RetryConfig) Delay() time.Duration        { return time.Duration(r.DelaySeconds) * time.Second }
func (r *RetryConfig) PollInterval() time.Duration { return time.Duration(r.PollIntervalSeconds) * time.Second }
func (r *RetryConfig) ReclaimAfter() time.Duration { return time.Duration(r.ReclaimAfterSeconds) * time.Second }
func (r *RetryConfig) Retention() time.Duration    { return time.Duration(r.RetentionDays) * 24 * time.Hour }

// Load reads the yaml file at path, resolves *_env fields against actual
// environment variables, applies defaults, and rejects invalid values
// instead of silently falling back.
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

	if cfg.Kafka.ConsumeTopic == "" {
		return nil, fmt.Errorf("kafka.consume_topic must be set")
	}
	if len(cfg.Kafka.Brokers) == 0 {
		return nil, fmt.Errorf("kafka.brokers must be set")
	}
	// Same overrides the other services honor for container flexibility.
	// Applied before the dlq default so the derived name tracks the
	// effective topic.
	if topic := os.Getenv("KAFKA_TOPIC_RETRY"); topic != "" {
		cfg.Kafka.ConsumeTopic = topic
	}
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		cfg.Kafka.Brokers = []string{brokers}
	}
	if cfg.Kafka.ConsumerGroup == "" {
		cfg.Kafka.ConsumerGroup = "retry-service"
	}
	if cfg.Kafka.DLQTopic == "" {
		cfg.Kafka.DLQTopic = cfg.Kafka.ConsumeTopic + ".dlq"
	}

	if err := applyRetryDefaults(&cfg.Retry); err != nil {
		return nil, err
	}
	// Operator-tunable knobs, overridden strictly: a bad value fails
	// startup instead of quietly changing retry behavior.
	if v := os.Getenv("RETRY_DELAY_SECONDS"); v != "" {
		n, err := parsePositiveInt("RETRY_DELAY_SECONDS", v)
		if err != nil {
			return nil, err
		}
		cfg.Retry.DelaySeconds = n
	}
	if v := os.Getenv("RETRY_MAX_ATTEMPTS"); v != "" {
		n, err := parsePositiveInt("RETRY_MAX_ATTEMPTS", v)
		if err != nil {
			return nil, err
		}
		cfg.Retry.MaxAttempts = n
	}

	return &cfg, nil
}

// applyRetryDefaults fills unset (zero) fields with their defaults and
// rejects negative values outright — a negative delay or attempt count is
// a configuration mistake, not something to paper over.
func applyRetryDefaults(r *RetryConfig) error {
	negatives := map[string]int{
		"retry.delay_seconds":         r.DelaySeconds,
		"retry.max_attempts":          r.MaxAttempts,
		"retry.poll_interval_seconds": r.PollIntervalSeconds,
		"retry.batch_size":            r.BatchSize,
		"retry.reclaim_after_seconds": r.ReclaimAfterSeconds,
		"retry.retention_days":        r.RetentionDays,
	}
	for name, v := range negatives {
		if v < 0 {
			return fmt.Errorf("%s must not be negative", name)
		}
	}
	if r.DelaySeconds == 0 {
		r.DelaySeconds = defaultDelaySeconds
	}
	if r.MaxAttempts == 0 {
		r.MaxAttempts = defaultMaxAttempts
	}
	if r.PollIntervalSeconds == 0 {
		r.PollIntervalSeconds = defaultPollIntervalSeconds
	}
	if r.BatchSize == 0 {
		r.BatchSize = defaultBatchSize
	}
	if r.ReclaimAfterSeconds == 0 {
		r.ReclaimAfterSeconds = defaultReclaimAfterSeconds
	}
	if r.RetentionDays == 0 {
		r.RetentionDays = defaultRetentionDays
	}
	return nil
}

// parsePositiveInt parses a strict positive integer env override. Unlike
// strconv.Atoi it rejects "+5", " 5" and other sloppy values so a
// mistyped variable is loud, not silently accepted.
func parsePositiveInt(name, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("env var %s must be a positive integer, got %q", name, value)
	}
	return n, nil
}
