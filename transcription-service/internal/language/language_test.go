package language

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"eng":      "eng",
		"ENG":      "eng",
		" en ":     "eng",
		"en-US":    "eng",
		"zh-Hant":  "zho",
		"hi":       "hin",
		"yue":      "yue",
		"tl":       "fil",
		"zz":       "",
		"":         "",
		"not-code": "not",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestISO1(t *testing.T) {
	for canonical, want := range map[string]string{
		"hin": "hi",
		"eng": "en",
		"fil": "tl",
		"yue": "",
	} {
		if got := ISO1(canonical); got != want {
			t.Fatalf("ISO1(%q) = %q, want %q", canonical, got, want)
		}
	}
}

func TestAllowed(t *testing.T) {
	if !Allowed("zho", nil) {
		t.Fatal("empty selection must allow everything")
	}
	if !Allowed("en", []string{"eng", "hin"}) {
		t.Fatal("2-letter detected code must match a canonical selection")
	}
	if !Allowed("eng", []string{"eng"}) {
		t.Fatal("exact match must be allowed")
	}
	if Allowed("zho", []string{"eng", "hin"}) {
		t.Fatal("unselected language must be rejected")
	}
	if Allowed("", []string{"eng"}) {
		t.Fatal("unknown/empty detected code must be rejected when filtering")
	}
}

func TestOmitReason(t *testing.T) {
	if got := OmitReason("hello", "eng", []string{"eng"}); got != "" {
		t.Fatalf("allowed transcript must be kept, got %q", got)
	}
	if got := OmitReason("   ", "eng", []string{"eng"}); got == "" {
		t.Fatal("blank transcript must be omitted")
	}
	if got := OmitReason("你好", "zho", []string{"eng"}); got == "" {
		t.Fatal("unselected language must be omitted")
	}
}
