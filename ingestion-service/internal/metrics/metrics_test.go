package metrics

import (
	"strings"
	"testing"
)

func TestCounterAndExposition(t *testing.T) {
	r := NewRegistry()
	r.Counter("uploads_created_total").Inc()
	r.Counter("uploads_created_total").Inc()
	r.Counter("http_requests_total", "method", "code").Inc("POST", "201")

	var b strings.Builder
	r.Write(&b)
	out := b.String()
	for _, want := range []string{
		"uploads_created_total 2",
		`http_requests_total{method="POST",code="201"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestHistogramBucketsCumulative(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("http_request_duration_seconds", []float64{0.1, 1}, "path")
	h.Observe(0.05, "/v1/uploads")
	h.Observe(0.5, "/v1/uploads")

	var b strings.Builder
	r.Write(&b)
	out := b.String()
	for _, want := range []string{
		`http_request_duration_seconds_bucket{path="/v1/uploads",le="0.1"} 1`,
		`http_request_duration_seconds_bucket{path="/v1/uploads",le="1"} 2`,
		`http_request_duration_seconds_bucket{path="/v1/uploads",le="+Inf"} 2`,
		`http_request_duration_seconds_count{path="/v1/uploads"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestGaugeSet(t *testing.T) {
	r := NewRegistry()
	r.Gauge("outbox_pending_total").Set(7)
	var b strings.Builder
	r.Write(&b)
	if !strings.Contains(b.String(), "outbox_pending_total 7") {
		t.Fatalf("missing gauge in:\n%s", b.String())
	}
}
