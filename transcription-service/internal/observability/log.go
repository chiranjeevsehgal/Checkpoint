// Package observability builds the process logger. Logs are JSON on stdout so
// the collector can ship them unparsed; every record carries the service name.
package observability

import (
	"log/slog"
	"os"
	"strings"
)

// New returns the process logger. LOG_LEVEL selects the minimum level
// (debug, info, warn, error; anything else defaults to info).
func New(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level()})
	return slog.New(handler).With("service", service)
}

func level() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
