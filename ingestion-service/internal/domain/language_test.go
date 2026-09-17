package domain

import (
	"errors"
	"testing"
)

func TestSupportedLanguagesAreCanonical(t *testing.T) {
	if len(SupportedLanguages) == 0 {
		t.Fatal("catalog must not be empty")
	}
	seen := map[string]bool{}
	for _, language := range SupportedLanguages {
		if len(language.Code) != 3 {
			t.Fatalf("%q is not an ISO-639-3 code", language.Code)
		}
		if language.Code != lowerASCII(language.Code) {
			t.Fatalf("%q must be lowercase", language.Code)
		}
		if seen[language.Code] {
			t.Fatalf("duplicate code %q", language.Code)
		}
		seen[language.Code] = true
	}
}

func TestValidateLanguages(t *testing.T) {
	got, err := ValidateLanguages([]string{})
	if err != nil || len(got) != 0 {
		t.Fatalf("empty selection: got %v, %v", got, err)
	}

	got, err = ValidateLanguages([]string{" ENG ", "hin", "eng", "HIN"})
	if err != nil {
		t.Fatalf("valid codes rejected: %v", err)
	}
	if len(got) != 2 || got[0] != "eng" || got[1] != "hin" {
		t.Fatalf("got %v, want [eng hin]", got)
	}

	for _, bad := range [][]string{{"xyz"}, {""}, {"  "}, {"eng", "not-a-code"}} {
		if _, err := ValidateLanguages(bad); !errors.Is(err, ErrInvalidLanguage) {
			t.Fatalf("%v: want ErrInvalidLanguage, got %v", bad, err)
		}
	}
}

func TestEverySupportedCodeValidates(t *testing.T) {
	for _, language := range SupportedLanguages {
		if _, err := ValidateLanguages([]string{language.Code}); err != nil {
			t.Fatalf("catalog code %q rejected: %v", language.Code, err)
		}
	}
}

func lowerASCII(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}
