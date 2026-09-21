package domain

import (
	"errors"
	"strings"
)

// ErrInvalidItemFilter is returned for an unknown todo status or reminder
// window on the items list endpoints.
var ErrInvalidItemFilter = errors.New("invalid item filter")

// ItemMaxTextLength bounds an edited item's text so the API cannot store
// unbounded content.
const ItemMaxTextLength = 1000

// ErrInvalidItemText is returned for empty or oversized edited item text.
var ErrInvalidItemText = errors.New("item text must be 1-1000 characters")

// ValidateItemText trims and validates edited text for a todo, reminder or
// insight.
func ValidateItemText(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(trimmed) > ItemMaxTextLength {
		return "", ErrInvalidItemText
	}
	return trimmed, nil
}
