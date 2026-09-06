package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setEnv(t *testing.T, k, v string) {
	t.Helper()
	t.Setenv(k, v)
}

func canonicalForTest(t *testing.T) string {
	t.Helper()
	topic, err := loadCanonicalTopic()
	if err != nil {
		t.Fatalf("config.yaml must load: %v", err)
	}
	if topic == "" {
		t.Fatal("canonical topic must not be empty")
	}
	return topic
}

func TestLoadProductionRequiresExplicit(t *testing.T) {
	canonical := canonicalForTest(t)
	setEnv(t, "ENV", "production")
	setEnv(t, "DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	setEnv(t, "MINIO_ACCESS_KEY", "ak")
	setEnv(t, "MINIO_SECRET_KEY", "sk")
	setEnv(t, "KAFKA_BROKERS", "kafka:9092")
	setEnv(t, "KAFKA_TOPIC_TRANSCRIPTION", canonical)
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

	// Unset topic in prod is fine: canonical config.yaml supplies it.
	setEnv(t, "KAFKA_TOPIC_TRANSCRIPTION", "")
	if _, err := Load(); err != nil {
		t.Fatalf("unset topic in prod must fall back to config.yaml: %v", err)
	}
}

func TestLoadTopicMismatchFails(t *testing.T) {
	canonicalForTest(t)
	setEnv(t, "ENV", "development")
	setEnv(t, "KAFKA_TOPIC_TRANSCRIPTION", "other.topic.v9")
	if _, err := Load(); err == nil {
		t.Fatal("env topic diverging from config.yaml must fail")
	}
}

func TestLoadMissingYAMLFails(t *testing.T) {
	setEnv(t, "CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	setEnv(t, "ENV", "development")
	if _, err := Load(); err == nil {
		t.Fatal("missing config.yaml must fail fast")
	}
}

func TestLoadKafkaDefaults(t *testing.T) {
	canonical := canonicalForTest(t)
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
	if cfg.KafkaTopic != canonical {
		t.Fatalf("topic must come from config.yaml %q, got %q", canonical, cfg.KafkaTopic)
	}
}

func TestLoadInvalidBoolAndPort(t *testing.T) {
	setEnv(t, "ENV", "development")
	setEnv(t, "KAFKA_TOPIC_TRANSCRIPTION", "")
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

// TestTopicSingleSourceOfTruth fails on drift between config.yaml and its
// mirrors. config.yaml is canonical; .env, .env.example and
// docker-compose.yaml must carry the same value, and config.go must not
// hardcode another topic literal.
func TestTopicSingleSourceOfTruth(t *testing.T) {
	canonical := canonicalForTest(t)

	repoRoot := filepath.Join("..", "..", "..")
	for _, name := range []string{".env", ".env.example"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		got := envFileTopic(t, string(raw), name)
		if got != canonical {
			t.Fatalf("%s KAFKA_TOPIC_TRANSCRIPTION=%q does not match config.yaml %q", name, got, canonical)
		}
	}

	compose, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yaml"))
	if err != nil {
		t.Fatalf("read docker-compose.yaml: %v", err)
	}
	assertComposeTopic(t, string(compose), canonical)

	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	if strings.Contains(string(src), `"transcription.`) {
		t.Fatal(`config.go must not hardcode a "transcription." topic literal; use config.yaml via loadCanonicalTopic()`)
	}
}

func envFileTopic(t *testing.T, content, name string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "KAFKA_TOPIC_TRANSCRIPTION=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "KAFKA_TOPIC_TRANSCRIPTION="))
		}
	}
	t.Fatalf("%s must define KAFKA_TOPIC_TRANSCRIPTION", name)
	return ""
}

func assertComposeTopic(t *testing.T, compose, canonical string) {
	t.Helper()
	if !strings.Contains(compose, "KAFKA_TOPIC_TRANSCRIPTION") {
		t.Fatal("docker-compose.yaml must reference KAFKA_TOPIC_TRANSCRIPTION")
	}
	// Fallback default ${KAFKA_TOPIC_TRANSCRIPTION:-<topic>} must match yaml.
	if marker := "${KAFKA_TOPIC_TRANSCRIPTION:-"; strings.Contains(compose, marker) {
		for _, line := range strings.Split(compose, "\n") {
			if i := strings.Index(line, marker); i >= 0 {
				rest := line[i+len(marker):]
				if j := strings.Index(rest, "}"); j >= 0 {
					if got := rest[:j]; got != canonical {
						t.Fatalf("compose fallback %q != config.yaml %q", got, canonical)
					}
				}
			}
		}
	}
	// No other hardcoded transcription topic may appear (kafka-init now uses env).
	for _, line := range strings.Split(compose, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, "--topic transcription.") && !strings.Contains(line, "KAFKA_TOPIC_TRANSCRIPTION") {
			t.Fatalf("compose must not hardcode --topic, use $KAFKA_TOPIC_TRANSCRIPTION: %q", strings.TrimSpace(line))
		}
	}
}
