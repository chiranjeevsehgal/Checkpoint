package observability

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func waitFor(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec,noctx
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server at %s never came up", url)
}

func TestHealthServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := NewHealthServer("127.0.0.1:18081")
	s.Start(ctx)
	waitFor(t, "http://127.0.0.1:18081/health/live")

	get := func(path string) (int, string) {
		t.Helper()
		resp, err := http.Get("http://127.0.0.1:18081" + path) //nolint:gosec,noctx
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	if code, _ := get("/health/live"); code != http.StatusOK {
		t.Fatalf("live = %d, want 200", code)
	}
	if code, _ := get("/health/ready"); code != http.StatusServiceUnavailable {
		t.Fatalf("ready before SetReady = %d, want 503", code)
	}
	s.SetReady(true)
	if code, _ := get("/health/ready"); code != http.StatusOK {
		t.Fatalf("ready after SetReady = %d, want 200", code)
	}
	s.Inc("test_events_total")
	s.Inc("test_events_total")
	code, body := get("/metrics")
	if code != http.StatusOK {
		t.Fatalf("metrics = %d, want 200", code)
	}
	if want := "test_events_total 2\n"; !strings.Contains(body, want) {
		t.Fatalf("metrics missing %q, got:\n%s", want, body)
	}
}
