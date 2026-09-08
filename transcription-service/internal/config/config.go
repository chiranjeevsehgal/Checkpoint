package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the full service configuration, loaded from config.yaml and overridden by environment variables where noted.
type Config struct {
	Provider  string          `yaml:"provider"`
	Providers ProvidersConfig `yaml:"providers"`
	Kafka     KafkaConfig     `yaml:"kafka"`
	MinIO     MinIOConfig     `yaml:"minio"`
	Postgres  PostgresConfig  `yaml:"postgres"`
}

type ProvidersConfig struct {
	Deepgram DeepgramConfig `yaml:"deepgram"`
}

type DeepgramConfig struct {
	APIKeyEnv string            `yaml:"api_key_env"`
	Model     string            `yaml:"model"`
	Language  string            `yaml:"language"`
	Options   map[string]string `yaml:"options"`
	apiKey    string
}

func (d *DeepgramConfig) APIKey() string {
	return d.apiKey
}

type KafkaConfig struct {
	Brokers                []string `yaml:"brokers"`
	ConsumeTopic           string   `yaml:"consume_topic"`
	ProduceTopicEmbedding  string   `yaml:"produce_topic1"`
	ProduceTopicExtraction string   `yaml:"produce_topic2"`
	ConsumerGroup          string   `yaml:"consumer_group"`
}

type MinIOConfig struct {
	Endpoint  string `yaml:"endpoint"`
	AccessKey string `yaml:"access_key_env"`
	SecretKey string `yaml:"secret_key_env"`
	UseSSL    bool   `yaml:"use_ssl"`
	accessKey string
	secretKey string
}

func (m *MinIOConfig) ResolvedAccessKey() string { return m.accessKey }
func (m *MinIOConfig) ResolvedSecretKey() string { return m.secretKey }

type PostgresConfig struct {
	DSNEnv string `yaml:"dsn_env"`
	dsn    string
}

func (p *PostgresConfig) DSN() string { return p.dsn }

// Load reads the yaml file at path, then resolves any *_env fields against actual environment variables.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.Providers.Deepgram.apiKey = os.Getenv(cfg.Providers.Deepgram.APIKeyEnv)
	if cfg.Provider == "deepgram" && cfg.Providers.Deepgram.apiKey == "" {
		return nil, fmt.Errorf("env var %s is empty but provider is set to deepgram", cfg.Providers.Deepgram.APIKeyEnv)
	}

	cfg.MinIO.accessKey = os.Getenv(cfg.MinIO.AccessKey)
	cfg.MinIO.secretKey = os.Getenv(cfg.MinIO.SecretKey)

	cfg.Postgres.dsn = os.Getenv(cfg.Postgres.DSNEnv)

	// Allow overriding Kafka brokers via env for container/orchestration flexibility
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		cfg.Kafka.Brokers = []string{brokers}
	}

	return &cfg, nil
}