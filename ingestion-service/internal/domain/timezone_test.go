package domain

import (
	"errors"
	"testing"
)

func TestValidateTimezoneAcceptsIANA(t *testing.T) {
	cases := map[string]string{
		"Europe/Berlin":   "Europe/Berlin",
		" Asia/Kolkata ":  "Asia/Kolkata",
		"America/New_York": "America/New_York",
		"UTC":             "UTC",
	}
	for input, want := range cases {
		got, err := ValidateTimezone(input)
		if err != nil {
			t.Fatalf("%q: unexpected error %v", input, err)
		}
		if got != want {
			t.Fatalf("%q: got %q, want %q", input, got, want)
		}
	}
}

func TestValidateTimezoneRejectsInvalid(t *testing.T) {
	for _, input := range []string{"", "   ", "Not/AZone", "GMT+1"} {
		if _, err := ValidateTimezone(input); !errors.Is(err, ErrInvalidTimezone) {
			t.Fatalf("%q: want ErrInvalidTimezone, got %v", input, err)
		}
	}
}
