package domain

import (
	"errors"
	"strings"
)

// McpKeyMaxNameLength bounds the human-readable label so the settings list
// cannot be used to store unbounded text.
const McpKeyMaxNameLength = 100

// ErrInvalidKeyName is returned for an empty or oversized MCP key label.
var ErrInvalidKeyName = errors.New("key name must be 1-100 characters")

// ValidateMcpKeyName trims and validates a key label.
func ValidateMcpKeyName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > McpKeyMaxNameLength {
		return "", ErrInvalidKeyName
	}
	return trimmed, nil
}
