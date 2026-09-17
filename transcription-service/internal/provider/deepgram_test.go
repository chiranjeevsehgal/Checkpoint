package provider

import "testing"

func TestDeepgramParams(t *testing.T) {
	options := map[string]string{"smart_format": "true"}

	t.Run("no selection keeps fallback", func(t *testing.T) {
		params := deepgramParams("nova-3", "multi", options, nil)
		if got := params.Get("language"); got != "multi" {
			t.Fatalf("language: got %q, want multi", got)
		}
		if got := params.Get("smart_format"); got != "true" {
			t.Fatalf("option lost: %q", got)
		}
	})

	t.Run("single supported language is forced", func(t *testing.T) {
		params := deepgramParams("nova-3", "multi", options, []string{"hin"})
		if got := params.Get("language"); got != "hi" {
			t.Fatalf("language: got %q, want hi", got)
		}
		if len(params["detect_language"]) != 0 {
			t.Fatalf("unexpected detect_language: %v", params["detect_language"])
		}
	})

	t.Run("single unsupported language falls back", func(t *testing.T) {
		params := deepgramParams("nova-3", "multi", options, []string{"yue"})
		if got := params.Get("language"); got != "multi" {
			t.Fatalf("language: got %q, want multi fallback", got)
		}
	})

	t.Run("multiple supported languages restrict detection", func(t *testing.T) {
		params := deepgramParams("nova-3", "multi", options, []string{"eng", "hin"})
		if params.Get("language") != "" {
			t.Fatalf("language must be unset when restricting detection, got %q", params.Get("language"))
		}
		got := params["detect_language"]
		if len(got) != 2 || got[0] != "en" || got[1] != "hi" {
			t.Fatalf("detect_language: got %v, want [en hi]", got)
		}
	})

	t.Run("multiple with an unsupported language falls back", func(t *testing.T) {
		params := deepgramParams("nova-3", "multi", options, []string{"eng", "yue"})
		if got := params.Get("language"); got != "multi" {
			t.Fatalf("language: got %q, want multi fallback", got)
		}
		if len(params["detect_language"]) != 0 {
			t.Fatalf("unexpected detect_language: %v", params["detect_language"])
		}
	})
}
