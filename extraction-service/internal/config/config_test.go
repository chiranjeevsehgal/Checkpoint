package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testYAML = `
kafka:
  brokers: [kafka:9092]
  consume_topic: extraction.jobs.v1
postgres:
  dsn_env: POSTGRES_DSN
groq:
  api_key_env: GROQ_API_KEY
batch:
  size: 10
`

func writeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(testYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")

	cfg, err := Load(writeConfig(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Groq.Model != "openai/gpt-oss-120b" {
		t.Fatalf("model default wrong: %s", cfg.Groq.Model)
	}
	if cfg.Groq.BaseURL != "https://api.groq.com/openai/v1" {
		t.Fatalf("base url default wrong: %s", cfg.Groq.BaseURL)
	}
	if cfg.Kafka.DLQTopic != "extraction.jobs.v1.dlq" {
		t.Fatalf("dlq default wrong: %s", cfg.Kafka.DLQTopic)
	}
	if cfg.Kafka.ConsumerGroup != "extraction-service" {
		t.Fatalf("consumer group default wrong: %s", cfg.Kafka.ConsumerGroup)
	}
	if cfg.Batch.MaxWaitSeconds != 0 {
		t.Fatalf("max wait default should be 0 (strict exactly-N), got %d", cfg.Batch.MaxWaitSeconds)
	}
}

func TestLoadRequiresDSNAndAPIKey(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "")
	t.Setenv("GROQ_API_KEY", "")
	if _, err := Load(writeConfig(t)); err == nil {
		t.Fatal("expected load to fail without POSTGRES_DSN/GROQ_API_KEY")
	}
}

func TestLoadEnvOverridesBrokers(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")
	t.Setenv("KAFKA_BROKERS", "kafka2:9092")

	cfg, err := Load(writeConfig(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Kafka.Brokers) != 1 || cfg.Kafka.Brokers[0] != "kafka2:9092" {
		t.Fatalf("broker override not applied: %v", cfg.Kafka.Brokers)
	}
}

func TestLoadRejectsInvalidBatchSize(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")

	path := filepath.Join(t.TempDir(), "config.yaml")
	bad := "kafka:\n  brokers: [k:9092]\n  consume_topic: t\npostgres:\n  dsn_env: POSTGRES_DSN\ngroq:\n  api_key_env: GROQ_API_KEY\nbatch:\n  size: 0\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected batch.size 0 to be rejected")
	}
}
