package config

import (
	"testing"
)

func setEnv(t *testing.T, k, v string) {
	t.Helper()
	t.Setenv(k, v)
}

func TestLoadProductionRequiresExplicit(t *testing.T) {
	setEnv(t, "ENV", "production")
	setEnv(t, "DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	setEnv(t, "MINIO_ACCESS_KEY", "ak")
	setEnv(t, "MINIO_SECRET_KEY", "sk")
	setEnv(t, "KAFKA_BROKERS", "kafka:9092")
	setEnv(t, "KAFKA_TOPIC_TRANSCRIPTION", "transcription.jobs.v1")
	setEnv(t, "PORT", "8080")
	if _, err := Load(); err != nil {
		t.Fatalf("valid prod must load: %v", err)
	}

	setEnv(t, "DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing DATABASE_URL in prod must fail")
	}
	setEnv(t, "DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")

	setEnv(t, "KAFKA_BROKERS", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing KAFKA_BROKERS in prod must fail")
	}
	setEnv(t, "KAFKA_BROKERS", "kafka:9092")

	setEnv(t, "KAFKA_TOPIC_TRANSCRIPTION", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing KAFKA_TOPIC_TRANSCRIPTION in prod must fail")
	}
}

func TestLoadKafkaDefaults(t *testing.T) {
	setEnv(t, "ENV", "development")
	setEnv(t, "KAFKA_BROKERS", "")
	setEnv(t, "KAFKA_TOPIC_TRANSCRIPTION", "")
	setEnv(t, "KAFKA_CLIENT_ID", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("dev defaults must load: %v", err)
	}
	if cfg.KafkaBrokers != "kafka:9092" {
		t.Fatalf("brokers default: got %q", cfg.KafkaBrokers)
	}
	if cfg.KafkaTopic != "transcription.jobs.v1" {
		t.Fatalf("topic default: got %q", cfg.KafkaTopic)
	}
}

func TestLoadInvalidBoolAndPort(t *testing.T) {
	setEnv(t, "ENV", "development")
	setEnv(t, "MINIO_USE_SSL", "banana")
	if _, err := Load(); err == nil {
		t.Fatal("invalid bool must fail")
	}
	setEnv(t, "MINIO_USE_SSL", "true")
	setEnv(t, "PORT", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("invalid PORT must fail")
	}
	setEnv(t, "PORT", "8080")
	setEnv(t, "UPLOAD_EXPIRY_HOURS", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("negative duration must fail")
	}
}
