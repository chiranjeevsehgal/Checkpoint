package domain

import (
	"errors"
	"testing"
)

func TestValidateAdvanceMinutesAcceptsOptions(t *testing.T) {
	for _, minutes := range []int{5, 10, 15, 20, 25, 30} {
		seconds, err := ValidateAdvanceMinutes(minutes)
		if err != nil {
			t.Fatalf("%d: %v", minutes, err)
		}
		if seconds != minutes*60 {
			t.Fatalf("%d -> %d seconds, want %d", minutes, seconds, minutes*60)
		}
	}
}

func TestValidateAdvanceMinutesRejectsOthers(t *testing.T) {
	for _, minutes := range []int{0, 4, 7, 31, -5} {
		if _, err := ValidateAdvanceMinutes(minutes); !errors.Is(err, ErrInvalidAdvance) {
			t.Fatalf("%d: want ErrInvalidAdvance, got %v", minutes, err)
		}
	}
}

func TestAdvanceMinutesFromSeconds(t *testing.T) {
	if got := AdvanceMinutesFromSeconds(0); got != DefaultAdvanceMinutes {
		t.Fatalf("unset = %d, want %d", got, DefaultAdvanceMinutes)
	}
	if got := AdvanceMinutesFromSeconds(1800); got != 30 {
		t.Fatalf("1800 seconds = %d minutes, want 30", got)
	}
}
