package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testYAML = `
postgres:
  dsn_env: POSTGRES_DSN
ntfy:
  token_env: NTFY_TOKEN
`

func writeConfig(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(testYAML+"\n"+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("NTFY_TOKEN", "tk_test")

	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Postgres.DSN() != "postgres://x" {
		t.Fatalf("dsn = %q", cfg.Postgres.DSN())
	}
	if cfg.Ntfy.Token() != "tk_test" {
		t.Fatalf("token = %q", cfg.Ntfy.Token())
	}
	if cfg.Ntfy.URL != defaultNtfyURL {
		t.Fatalf("url default = %q, want %q", cfg.Ntfy.URL, defaultNtfyURL)
	}
	if cfg.Ntfy.Advance() != 15*time.Minute {
		t.Fatalf("advance default = %s, want 15m", cfg.Ntfy.Advance())
	}
	if cfg.Ntfy.AdvanceMaxSeconds != defaultAdvanceMaxSeconds {
		t.Fatalf("advance_max default = %d, want %d", cfg.Ntfy.AdvanceMaxSeconds, defaultAdvanceMaxSeconds)
	}
	if cfg.Delivery.PollInterval() != 15*time.Second {
		t.Fatalf("poll default = %s, want 15s", cfg.Delivery.PollInterval())
	}
	if cfg.Delivery.WorkerConcurrency != defaultWorkerParallel {
		t.Fatalf("worker_concurrency = %d, want %d", cfg.Delivery.WorkerConcurrency, defaultWorkerParallel)
	}
	if cfg.Delivery.MaxAttempts != defaultMaxAttempts {
		t.Fatalf("max_attempts = %d, want %d", cfg.Delivery.MaxAttempts, defaultMaxAttempts)
	}
}

func TestLoadRequiresDSNAndToken(t *testing.T) {
	t.Setenv("NTFY_TOKEN", "tk_test")
	t.Setenv("POSTGRES_DSN", "")
	if _, err := Load(writeConfig(t, "")); err == nil {
		t.Fatal("missing POSTGRES_DSN must fail")
	}

	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("NTFY_TOKEN", "")
	if _, err := Load(writeConfig(t, "")); err == nil {
		t.Fatal("missing NTFY_TOKEN must fail")
	}
}

func TestLoadRejectsAdvanceNotBeyondGrace(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("NTFY_TOKEN", "tk_test")

	extra := `
ntfy:
  advance_seconds: 120
delivery:
  advance_grace_seconds: 120
`
	if _, err := Load(writeConfig(t, extra)); err == nil {
		t.Fatal("advance <= grace must fail")
	}
}
