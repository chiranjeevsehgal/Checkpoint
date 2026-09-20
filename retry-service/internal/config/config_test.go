package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testYAML = `
kafka:
  brokers: [kafka:9092]
  consume_topic: retry.jobs.v1
postgres:
  dsn_env: POSTGRES_DSN
retry:
  delay_seconds: 1800
  max_attempts: 2
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
	t.Setenv("KAFKA_BROKERS", "")

	cfg, err := Load(writeConfig(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Retry.PollIntervalSeconds != 10 {
		t.Fatalf("poll interval default wrong: %d", cfg.Retry.PollIntervalSeconds)
	}
	if cfg.Retry.BatchSize != 50 {
		t.Fatalf("batch size default wrong: %d", cfg.Retry.BatchSize)
	}
	if cfg.Retry.ReclaimAfterSeconds != 300 {
		t.Fatalf("reclaim default wrong: %d", cfg.Retry.ReclaimAfterSeconds)
	}
	if cfg.Retry.RetentionDays != 30 {
		t.Fatalf("retention default wrong: %d", cfg.Retry.RetentionDays)
	}
	if cfg.Retry.Delay() != 30*time.Minute {
		t.Fatalf("delay default wrong: %v", cfg.Retry.Delay())
	}
}

func TestLoadRequiresDSN(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "")
	if _, err := Load(writeConfig(t)); err == nil {
		t.Fatal("expected load to fail without POSTGRES_DSN")
	}
}

func TestLoadRequiresConsumeTopic(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("KAFKA_TOPIC_RETRY", "")

	path := filepath.Join(t.TempDir(), "config.yaml")
	bad := "kafka:\n  brokers: [k:9092]\npostgres:\n  dsn_env: POSTGRES_DSN\nretry:\n  delay_seconds: 60\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected load to fail without kafka.consume_topic")
	}
}

func TestLoadEnvOverridesBrokersAndTopic(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("KAFKA_BROKERS", "kafka2:9092")
	t.Setenv("KAFKA_TOPIC_RETRY", "retry.jobs.v2")

	cfg, err := Load(writeConfig(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Kafka.Brokers) != 1 || cfg.Kafka.Brokers[0] != "kafka2:9092" {
		t.Fatalf("broker override not applied: %v", cfg.Kafka.Brokers)
	}
	if cfg.Kafka.ConsumeTopic != "retry.jobs.v2" {
		t.Fatalf("topic override not applied: %s", cfg.Kafka.ConsumeTopic)
	}
	if cfg.Kafka.DLQTopic != "retry.jobs.v2.dlq" {
		t.Fatalf("dlq should derive from overridden topic: %s", cfg.Kafka.DLQTopic)
	}
}

func TestLoadDefaultsDLQAndGroup(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("KAFKA_BROKERS", "")
	t.Setenv("KAFKA_TOPIC_RETRY", "")

	path := filepath.Join(t.TempDir(), "config.yaml")
	minimal := "kafka:\n  brokers: [k:9092]\n  consume_topic: retry.jobs.v1\npostgres:\n  dsn_env: POSTGRES_DSN\n"
	if err := os.WriteFile(path, []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Kafka.DLQTopic != "retry.jobs.v1.dlq" {
		t.Fatalf("dlq default wrong: %s", cfg.Kafka.DLQTopic)
	}
	if cfg.Kafka.ConsumerGroup != "retry-service" {
		t.Fatalf("consumer group default wrong: %s", cfg.Kafka.ConsumerGroup)
	}
}

func TestLoadRetryEnvOverrides(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("RETRY_DELAY_SECONDS", "60")
	t.Setenv("RETRY_MAX_ATTEMPTS", "5")

	cfg, err := Load(writeConfig(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Retry.DelaySeconds != 60 {
		t.Fatalf("delay override not applied: %d", cfg.Retry.DelaySeconds)
	}
	if cfg.Retry.MaxAttempts != 5 {
		t.Fatalf("max attempts override not applied: %d", cfg.Retry.MaxAttempts)
	}
}

func TestLoadRejectsInvalidRetryEnvOverrides(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("RETRY_MAX_ATTEMPTS", "zero")

	if _, err := Load(writeConfig(t)); err == nil {
		t.Fatal("expected non-numeric RETRY_MAX_ATTEMPTS to be rejected")
	}
}

func TestLoadRejectsNegativeRetryValues(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")

	path := filepath.Join(t.TempDir(), "config.yaml")
	bad := "kafka:\n  brokers: [k:9092]\n  consume_topic: t\npostgres:\n  dsn_env: POSTGRES_DSN\nretry:\n  delay_seconds: -5\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected negative retry.delay_seconds to be rejected")
	}
}
