package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	_ "time/tzdata" // embedded IANA database so LoadLocation works on any host OS

	"gopkg.in/yaml.v3"
)

const (
	defaultGroqModel      = "openai/gpt-oss-120b"
	defaultGroqBaseURL    = "https://api.groq.com/openai/v1"
	defaultMaxInputChars  = 24000
	defaultWorkerParallel = 3
	defaultPollSeconds    = 60
)

// Config is the full rollup-service configuration, loaded from config.yaml with
// environment overrides for container/orchestration flexibility.
type Config struct {
	Postgres   PostgresConfig   `yaml:"postgres"`
	Groq       GroqConfig       `yaml:"groq"`
	Summaries  SummariesConfig  `yaml:"summaries"`
	Processing ProcessingConfig `yaml:"processing"`
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

func (g *GroqConfig) APIKey() string         { return g.apiKey }
func (g *GroqConfig) Timeout() time.Duration { return time.Duration(g.TimeoutSeconds) * time.Second }

// SummariesConfig controls how source material is turned into narratives and
// how many users are summarized in parallel.
type SummariesConfig struct {
	MaxInputChars     int    `yaml:"max_input_chars"`
	LookbackDays      int    `yaml:"lookback_days"`
	Timezone          string `yaml:"timezone"`
	WorkerConcurrency int    `yaml:"worker_concurrency"`
	loc               *time.Location
}

func (s *SummariesConfig) Loc() *time.Location { return s.loc }

// ProcessingConfig gates the worker to a nightly window.
type ProcessingConfig struct {
	WindowEnabled       bool   `yaml:"window_enabled"`
	WindowStart         string `yaml:"window_start"` // "HH:MM" 24h
	WindowEnd           string `yaml:"window_end"`   // "HH:MM" 24h
	WindowTimezone      string `yaml:"window_timezone"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
	startMinutes        int
	endMinutes          int
	loc                 *time.Location
}

// Loc returns the window timezone.
func (p *ProcessingConfig) Loc() *time.Location { return p.loc }

// PollInterval is how often the worker checks whether the window is open.
func (p *ProcessingConfig) PollInterval() time.Duration {
	return time.Duration(p.PollIntervalSeconds) * time.Second
}

// InWindow reports whether now falls inside the configured window. start > end
// wraps past midnight; start == end is rejected at load. Disabled windows are
// always "open" so the service runs immediately.
func (p *ProcessingConfig) InWindow(now time.Time) bool {
	if !p.WindowEnabled || p.loc == nil {
		return true
	}
	local := now.In(p.loc)
	minutes := local.Hour()*60 + local.Minute()
	if p.startMinutes < p.endMinutes {
		return minutes >= p.startMinutes && minutes < p.endMinutes
	}
	return minutes >= p.startMinutes || minutes < p.endMinutes
}

// parseClock converts "HH:MM" into minutes since midnight.
func parseClock(value string) (int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("invalid time %q (want HH:MM 24h)", value)
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

// Load reads the yaml file at path, resolves *_env fields against actual
// environment variables, applies defaults, and rejects invalid values instead
// of silently falling back.
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
		cfg.Groq.MaxCompletionTokens = 2048
	}

	if cfg.Summaries.MaxInputChars <= 0 {
		cfg.Summaries.MaxInputChars = defaultMaxInputChars
	}
	if cfg.Summaries.LookbackDays < 0 {
		return nil, fmt.Errorf("summaries.lookback_days must be >= 0")
	}
	if cfg.Summaries.WorkerConcurrency <= 0 {
		cfg.Summaries.WorkerConcurrency = defaultWorkerParallel
	}
	if tz := os.Getenv("SUMMARIES_TIMEZONE"); tz != "" {
		cfg.Summaries.Timezone = tz
	}
	if cfg.Summaries.Timezone == "" {
		cfg.Summaries.Timezone = "UTC"
	}
	summaryLoc, err := time.LoadLocation(cfg.Summaries.Timezone)
	if err != nil {
		return nil, fmt.Errorf("summaries.timezone %q is not a valid IANA zone: %w", cfg.Summaries.Timezone, err)
	}
	cfg.Summaries.loc = summaryLoc

	if cfg.Processing.PollIntervalSeconds <= 0 {
		cfg.Processing.PollIntervalSeconds = defaultPollSeconds
	}
	if cfg.Processing.WindowEnabled {
		if cfg.Processing.WindowStart == "" || cfg.Processing.WindowEnd == "" {
			return nil, fmt.Errorf("processing.window_start and window_end are required when window_enabled")
		}
		start, err := parseClock(cfg.Processing.WindowStart)
		if err != nil {
			return nil, fmt.Errorf("processing.window_start: %w", err)
		}
		end, err := parseClock(cfg.Processing.WindowEnd)
		if err != nil {
			return nil, fmt.Errorf("processing.window_end: %w", err)
		}
		if start == end {
			return nil, fmt.Errorf("processing window_start and window_end must differ")
		}
		tz := cfg.Processing.WindowTimezone
		if tz == "" {
			tz = cfg.Summaries.Timezone
		}
		windowLoc, err := time.LoadLocation(tz)
		if err != nil {
			return nil, fmt.Errorf("processing.window_timezone %q is not a valid IANA zone: %w", tz, err)
		}
		cfg.Processing.startMinutes = start
		cfg.Processing.endMinutes = end
		cfg.Processing.loc = windowLoc
	}

	return &cfg, nil
}
