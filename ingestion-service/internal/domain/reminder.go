package domain

import (
	"errors"
	"fmt"
)

// ErrInvalidAdvance means the advance lead is not one of the supported choices.
var ErrInvalidAdvance = errors.New("invalid advance time")

// advanceMinuteOptions is the app's selectable advance lead, in minutes.
var advanceMinuteOptions = []int{5, 10, 15, 20, 25, 30}

// DefaultAdvanceMinutes is used when the user has not chosen a lead time.
const DefaultAdvanceMinutes = 15

// ValidateAdvanceMinutes maps a supported minute value to seconds.
func ValidateAdvanceMinutes(minutes int) (int, error) {
	for _, option := range advanceMinuteOptions {
		if minutes == option {
			return minutes * 60, nil
		}
	}
	return 0, fmt.Errorf("%w: %d", ErrInvalidAdvance, minutes)
}

// AdvanceMinutesFromSeconds returns the display minutes for a stored value,
// falling back to the default when unset.
func AdvanceMinutesFromSeconds(seconds int) int {
	if seconds <= 0 {
		return DefaultAdvanceMinutes
	}
	return seconds / 60
}
