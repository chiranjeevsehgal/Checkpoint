package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	setEnv(t, "DATABASE_REQUEST_URL", "postgres://request:p@localhost:5432/db?sslmode=disable")
	setEnv(t, "DATABASE_WORKER_URL", "postgres://worker:p@localhost:5432/db?sslmode=disable")
	setEnv(t, "KRATOS_PUBLIC_URL", "http://kratos:4433")
	setEnv(t, "MINIO_ACCESS_KEY", "ak")
	setEnv(t, "MINIO_SECRET_KEY", "sk")
	setEnv(t, "KAFKA_BROKERS", "kafka:9092")
	setEnv(t, "NTFY_PUBLIC_URL", "https://ntfy.example.com")
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

	setEnv(t, "DATABASE_REQUEST_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing DATABASE_REQUEST_URL in prod must fail")
	}
	setEnv(t, "DATABASE_REQUEST_URL", "postgres://request:p@localhost:5432/db?sslmode=disable")

	setEnv(t, "KRATOS_PUBLIC_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing KRATOS_PUBLIC_URL in prod must fail")
	}
	setEnv(t, "KRATOS_PUBLIC_URL", "http://kratos:4433")

	setEnv(t, "KAFKA_BROKERS", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing KAFKA_BROKERS in prod must fail")
	}
	setEnv(t, "KAFKA_BROKERS", "kafka:9092")

	setEnv(t, "NTFY_PUBLIC_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing NTFY_PUBLIC_URL in prod must fail")
	}
	setEnv(t, "NTFY_PUBLIC_URL", "https://ntfy.example.com")

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

func TestLoadInvalidTopicNameFails(t *testing.T) {
	for _, topic := range []string{"bad topic!", "has/slash", "has space", "semi;colon", string(make([]byte, 0))} {
		if topic == "" {
			continue
		}
		path := filepath.Join(t.TempDir(), "config.yaml")
		content := "kafka:\n  topic_transcription: " + topic + "\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		setEnv(t, "CONFIG_FILE", path)
		setEnv(t, "ENV", "development")
		setEnv(t, "KAFKA_TOPIC_TRANSCRIPTION", "")
		if _, err := Load(); err == nil {
			t.Fatalf("invalid topic %q must fail", topic)
		}
	}
	long := string(make([]byte, 0))
	for i := 0; i < 250; i++ {
		long += "a"
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("kafka:\n  topic_transcription: "+long+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setEnv(t, "CONFIG_FILE", path)
	if _, err := Load(); err == nil {
		t.Fatal("overlong topic must fail")
	}
	_ = long
}

func TestLoadMalformedYAMLFailsFast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("kafka:\n  topic_transcription: [unclosed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setEnv(t, "CONFIG_FILE", path)
	setEnv(t, "ENV", "development")
	if _, err := Load(); err == nil {
		t.Fatal("malformed config.yaml must fail fast")
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
	setEnv(t, "UPLOAD_EXPIRY_HOURS", "1")
	setEnv(t, "IDENTITY_TTL_HOURS", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("invalid identity TTL must fail")
	}
}

func TestLoadIdentityCleanupDefaults(t *testing.T) {
	canonicalForTest(t)
	setEnv(t, "ENV", "development")
	setEnv(t, "IDENTITY_TTL_HOURS", "")
	setEnv(t, "IDENTITY_CLEANUP_INTERVAL_MINUTES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("dev defaults must load: %v", err)
	}
	if cfg.IdentityTTL != time.Hour {
		t.Fatalf("identity TTL = %v, want 1h", cfg.IdentityTTL)
	}
	if cfg.IdentityCleanupInterval != 15*time.Minute {
		t.Fatalf("cleanup interval = %v, want 15m", cfg.IdentityCleanupInterval)
	}
}

// TestTopicSingleSourceOfTruth fails on drift between config.yaml and its
// mirrors. config.yaml is canonical; .env.example and docker-compose.yaml
// must carry the same value, and config.go must not hardcode another topic
// literal. Local .env (gitignored) is checked only when present.
func TestTopicSingleSourceOfTruth(t *testing.T) {
	canonical := canonicalForTest(t)

	repoRoot := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	if got := envFileTopic(t, string(raw), ".env.example"); got != canonical {
		t.Fatalf(".env.example KAFKA_TOPIC_TRANSCRIPTION=%q does not match config.yaml %q", got, canonical)
	}
	if raw, err := os.ReadFile(filepath.Join(repoRoot, ".env")); err == nil {
		if got := envFileTopic(t, string(raw), ".env"); got != canonical {
			t.Fatalf(".env KAFKA_TOPIC_TRANSCRIPTION=%q does not match config.yaml %q", got, canonical)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("read .env: %v", err)
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
