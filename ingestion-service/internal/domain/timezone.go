package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	_ "time/tzdata" // embedded IANA database so LoadLocation works on any host OS
)

// ErrInvalidTimezone means the value is not a resolvable IANA zone id.
var ErrInvalidTimezone = errors.New("invalid IANA timezone")

// ValidateTimezone trims and verifies an IANA zone id such as "Europe/Berlin".
func ValidateTimezone(timezone string) (string, error) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return "", ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidTimezone, err)
	}
	return timezone, nil
}
