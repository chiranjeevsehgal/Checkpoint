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
groq:
  api_key_env: GROQ_API_KEY
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
	t.Setenv("GROQ_API_KEY", "gsk_test")
	t.Setenv("SUMMARIES_TIMEZONE", "")

	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Groq.Model != "openai/gpt-oss-120b" {
		t.Fatalf("model default wrong: %s", cfg.Groq.Model)
	}
	if cfg.Groq.MaxAttempts != 3 {
		t.Fatalf("max_attempts default = %d, want 3", cfg.Groq.MaxAttempts)
	}
	if cfg.Summaries.MaxInputChars != 24000 {
		t.Fatalf("max_input_chars default = %d, want 24000", cfg.Summaries.MaxInputChars)
	}
	if cfg.Summaries.WorkerConcurrency != 3 {
		t.Fatalf("worker_concurrency default = %d, want 3", cfg.Summaries.WorkerConcurrency)
	}
	if cfg.Summaries.Loc() != time.UTC {
		t.Fatalf("timezone default = %v, want UTC", cfg.Summaries.Loc())
	}
	if !cfg.Processing.InWindow(time.Now()) {
		t.Fatal("a disabled window must always be open")
	}
}

func TestLoadRequiresDSNAndAPIKey(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "")
	t.Setenv("GROQ_API_KEY", "")
	if _, err := Load(writeConfig(t, "")); err == nil {
		t.Fatal("expected load to fail without POSTGRES_DSN/GROQ_API_KEY")
	}
}

func TestLoadAppliesGroqMaxAttemptsOverride(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := `
postgres:
  dsn_env: POSTGRES_DSN
groq:
  api_key_env: GROQ_API_KEY
  max_attempts: 5
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Groq.MaxAttempts != 5 {
		t.Fatalf("max_attempts = %d, want 5", cfg.Groq.MaxAttempts)
	}
}

func TestLoadRejectsNegativeLookback(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")
	if _, err := Load(writeConfig(t, "summaries:\n  lookback_days: -1\n")); err == nil {
		t.Fatal("expected negative lookback_days to be rejected")
	}
}

func TestInWindowNonWrapping(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")
	cfg, err := Load(writeConfig(t, `
processing:
  window_enabled: true
  window_start: "01:00"
  window_end: "05:00"
  window_timezone: UTC
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before", time.Date(2026, 9, 18, 0, 59, 0, 0, time.UTC), false},
		{"start inclusive", time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC), true},
		{"inside", time.Date(2026, 9, 18, 3, 30, 0, 0, time.UTC), true},
		{"end exclusive", time.Date(2026, 9, 18, 5, 0, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cfg.Processing.InWindow(c.at); got != c.want {
				t.Fatalf("InWindow(%v) = %v, want %v", c.at, got, c.want)
			}
		})
	}
}

func TestInWindowWrapsMidnight(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")
	cfg, err := Load(writeConfig(t, `
processing:
  window_enabled: true
  window_start: "22:00"
  window_end: "06:00"
  window_timezone: UTC
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Processing.InWindow(time.Date(2026, 9, 18, 23, 0, 0, 0, time.UTC)) != true {
		t.Fatal("expected 23:00 inside the wrapping window")
	}
	if cfg.Processing.InWindow(time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)) != false {
		t.Fatal("expected midday outside the wrapping window")
	}
	if cfg.Processing.InWindow(time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)) != false {
		t.Fatal("expected the end boundary excluded")
	}
}

func TestWindowTimezoneDefaultsToSummariesTimezone(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")
	t.Setenv("SUMMARIES_TIMEZONE", "Asia/Kolkata")
	cfg, err := Load(writeConfig(t, `
processing:
  window_enabled: true
  window_start: "01:00"
  window_end: "05:00"
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// 20:30 UTC is 02:00 IST: inside the window in the summary zone.
	if !cfg.Processing.InWindow(time.Date(2026, 9, 17, 20, 30, 0, 0, time.UTC)) {
		t.Fatal("expected 02:00 IST to fall inside the window")
	}
}

func TestLoadRejectsInvalidWindow(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://x")
	t.Setenv("GROQ_API_KEY", "gsk_test")
	cases := map[string]string{
		"equal bounds":   "processing:\n  window_enabled: true\n  window_start: \"01:00\"\n  window_end: \"01:00\"\n",
		"invalid time":   "processing:\n  window_enabled: true\n  window_start: \"25:00\"\n  window_end: \"05:00\"\n",
		"invalid zone":   "processing:\n  window_enabled: true\n  window_start: \"01:00\"\n  window_end: \"05:00\"\n  window_timezone: \"Mars/Olympus\"\n",
		"missing bounds": "processing:\n  window_enabled: true\n",
	}
	for name, extra := range cases {
		if _, err := Load(writeConfig(t, extra)); err == nil {
			t.Fatalf("expected %s to be rejected", name)
		}
	}
}
