package config

import (
	"fmt"
	"os"
	"time"

	_ "time/tzdata" // embedded IANA database so LoadLocation works on any host OS

	"gopkg.in/yaml.v3"
)

const (
	defaultGroqModel   = "openai/gpt-oss-120b"
	defaultGroqBaseURL = "https://api.groq.com/openai/v1"
)

// Config is the full service configuration, loaded from config.yaml with
// environment overrides for container/orchestration flexibility.
type Config struct {
	Kafka     KafkaConfig     `yaml:"kafka"`
	Postgres  PostgresConfig  `yaml:"postgres"`
	Groq      GroqConfig      `yaml:"groq"`
	Batch     BatchConfig     `yaml:"batch"`
	Reminders RemindersConfig `yaml:"reminders"`
}

type KafkaConfig struct {
	Brokers       []string `yaml:"brokers"`
	ConsumeTopic  string   `yaml:"consume_topic"`
	ConsumerGroup string   `yaml:"consumer_group"`
	DLQTopic      string   `yaml:"dlq_topic"`
	RetryTopic    string   `yaml:"retry_topic"`
}

type PostgresConfig struct {
	DSNEnv string `yaml:"dsn_env"`
	dsn    string
}

func (p *PostgresConfig) DSN() string { return p.dsn }

type GroqConfig struct {
	APIKeyEnv           string  `yaml:"api_key_env"`
	Model               string  `yaml:"model"`
	BaseURL             string  `yaml:"base_url"`
	TimeoutSeconds      int     `yaml:"timeout_seconds"`
	MaxCompletionTokens int     `yaml:"max_completion_tokens"`
	Temperature         float64 `yaml:"temperature"`
	TopP                float64 `yaml:"top_p"`
	ReasoningEffort     string  `yaml:"reasoning_effort"`
	apiKey              string
}

func (g *GroqConfig) APIKey() string          { return g.apiKey }
func (g *GroqConfig) Timeout() time.Duration  { return time.Duration(g.TimeoutSeconds) * time.Second }

type BatchConfig struct {
	Size                int `yaml:"size"`
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	MaxWaitSeconds      int `yaml:"max_wait_seconds"`
	WorkerConcurrency   int `yaml:"worker_concurrency"`
	MaxAttempts         int `yaml:"max_attempts"`
	ReclaimAfterSeconds int `yaml:"reclaim_after_seconds"`
	MaxBatchChars       int `yaml:"max_batch_chars"`
}

func (b *BatchConfig) PollInterval() time.Duration { return time.Duration(b.PollIntervalSeconds) * time.Second }
func (b *BatchConfig) MaxWait() time.Duration       { return time.Duration(b.MaxWaitSeconds) * time.Second }
func (b *BatchConfig) ReclaimAfter() time.Duration  { return time.Duration(b.ReclaimAfterSeconds) * time.Second }

// RemindersConfig carries the user's wall-clock timezone the reminder
// prompt resolves relative times in ("tomorrow at 5pm"). remind_at itself
// is always stored as a UTC instant.
type RemindersConfig struct {
	Timezone string `yaml:"timezone"`
	loc      *time.Location
}

func (r *RemindersConfig) Loc() *time.Location { return r.loc }

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

	cfg.Groq.apiKey = os.Getenv(cfg.Groq.APIKeyEnv)
	if cfg.Groq.apiKey == "" {
		return nil, fmt.Errorf("env var %s is empty", cfg.Groq.APIKeyEnv)
	}
	if cfg.Groq.Model == "" {
		cfg.Groq.Model = defaultGroqModel
	}
	if cfg.Groq.BaseURL == "" {
		cfg.Groq.BaseURL = defaultGroqBaseURL
	}
	if cfg.Groq.TimeoutSeconds <= 0 {
		cfg.Groq.TimeoutSeconds = 120
	}
	if cfg.Groq.MaxCompletionTokens <= 0 {
		cfg.Groq.MaxCompletionTokens = 8192
	}

	if cfg.Kafka.ConsumeTopic == "" {
		return nil, fmt.Errorf("kafka.consume_topic must be set")
	}
	if len(cfg.Kafka.Brokers) == 0 {
		return nil, fmt.Errorf("kafka.brokers must be set")
	}
	if cfg.Kafka.ConsumerGroup == "" {
		cfg.Kafka.ConsumerGroup = "extraction-service"
	}
	if cfg.Kafka.DLQTopic == "" {
		cfg.Kafka.DLQTopic = cfg.Kafka.ConsumeTopic + ".dlq"
	}
	// Handoffs go to the central retry service; same env override the other
	// services honor so compose can point them at one place.
	if topic := os.Getenv("KAFKA_TOPIC_RETRY"); topic != "" {
		cfg.Kafka.RetryTopic = topic
	}
	if cfg.Kafka.RetryTopic == "" {
		cfg.Kafka.RetryTopic = "retry.jobs.v1"
	}
	// Same override the other services honor for container flexibility.
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		cfg.Kafka.Brokers = []string{brokers}
	}

	if cfg.Batch.Size <= 0 {
		return nil, fmt.Errorf("batch.size must be positive")
	}
	if cfg.Batch.MaxWaitSeconds < 0 {
		return nil, fmt.Errorf("batch.max_wait_seconds must be >= 0")
	}
	if cfg.Batch.PollIntervalSeconds <= 0 {
		cfg.Batch.PollIntervalSeconds = 5
	}
	if cfg.Batch.WorkerConcurrency <= 0 {
		cfg.Batch.WorkerConcurrency = 3
	}
	if cfg.Batch.MaxAttempts <= 0 {
		cfg.Batch.MaxAttempts = 5
	}
	if cfg.Batch.ReclaimAfterSeconds <= 0 {
		cfg.Batch.ReclaimAfterSeconds = 300
	}
	if cfg.Batch.MaxBatchChars <= 0 {
		cfg.Batch.MaxBatchChars = 200000
	}

	// Reminder timezone: env wins, then yaml, then UTC. Validated here so a
	// typo fails fast at startup instead of producing wrong remind_at times.
	if tz := os.Getenv("REMINDERS_TIMEZONE"); tz != "" {
		cfg.Reminders.Timezone = tz
	}
	if cfg.Reminders.Timezone == "" {
		cfg.Reminders.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(cfg.Reminders.Timezone)
	if err != nil {
		return nil, fmt.Errorf("reminders.timezone %q is not a valid IANA zone: %w", cfg.Reminders.Timezone, err)
	}
	cfg.Reminders.loc = loc

	return &cfg, nil
}
