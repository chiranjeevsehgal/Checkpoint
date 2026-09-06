// Package metrics exposes Prometheus counters, histograms and gauges
// without external dependencies. It covers the LLD service-level
// signals; per-client library instrumentation is a later step.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry holds all series. One registry per process.
type Registry struct {
	mu         sync.Mutex
	counters   map[string]*Counter
	histograms map[string]*Histogram
	gauges     map[string]*Gauge
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]*Counter{},
		histograms: map[string]*Histogram{},
		gauges:     map[string]*Gauge{},
	}
}

// Counter counts events split by label values.
type Counter struct {
	name   string
	labels []string
	mu     sync.Mutex
	values map[string]float64
	keys   map[string][]string
}

// Counter registers (or returns) a counter.
func (r *Registry) Counter(name string, labels ...string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{name: name, labels: labels, values: map[string]float64{}, keys: map[string][]string{}}
	r.counters[name] = c
	return c
}

// Inc adds one to the series for these label values.
func (c *Counter) Inc(labelValues ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := strings.Join(labelValues, "\xff")
	c.values[k]++
	c.keys[k] = labelValues
}

// Histogram observes durations split by label values.
type Histogram struct {
	name    string
	labels  []string
	buckets []float64
	mu      sync.Mutex
	counts  map[string][]float64
	totals  map[string]float64
	sums    map[string]float64
	keys    map[string][]string
}

// DefaultLatencyBuckets covers 5ms to 10s.
var DefaultLatencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Histogram registers (or returns) a histogram.
func (r *Registry) Histogram(name string, buckets []float64, labels ...string) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.histograms[name]; ok {
		return h
	}
	h := &Histogram{name: name, labels: labels, buckets: buckets,
		counts: map[string][]float64{}, totals: map[string]float64{}, sums: map[string]float64{}, keys: map[string][]string{}}
	r.histograms[name] = h
	return h
}

// Observe records one sample in seconds.
func (h *Histogram) Observe(seconds float64, labelValues ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := strings.Join(labelValues, "\xff")
	buckets, ok := h.counts[k]
	if !ok {
		buckets = make([]float64, len(h.buckets))
		h.keys[k] = labelValues
	}
	for i, b := range h.buckets {
		if seconds <= b {
			buckets[i]++
		}
	}
	h.counts[k] = buckets
	h.totals[k]++
	h.sums[k] += seconds
}

// Gauge holds a settable value split by label values.
type Gauge struct {
	name   string
	labels []string
	mu     sync.Mutex
	values map[string]float64
	keys   map[string][]string
}

// Gauge registers (or returns) a gauge.
func (r *Registry) Gauge(name string, labels ...string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &Gauge{name: name, labels: labels, values: map[string]float64{}, keys: map[string][]string{}}
	r.gauges[name] = g
	return g
}

// Set replaces the series value.
func (g *Gauge) Set(v float64, labelValues ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := strings.Join(labelValues, "\xff")
	g.values[k] = v
	g.keys[k] = labelValues
}

// formatLabels renders label pairs for exposition.
func formatLabels(names, values []string) string {
	if len(names) == 0 {
		return ""
	}
	pairs := make([]string, len(names))
	for i, n := range names {
		v := ""
		if i < len(values) {
			v = values[i]
		}
		pairs[i] = fmt.Sprintf(`%s=%q`, n, v)
	}
	return "{" + strings.Join(pairs, ",") + "}"
}

// Write renders the Prometheus text exposition format.
func (r *Registry) Write(b *strings.Builder) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, name := range sortedKeys(r.counters) {
		c := r.counters[name]
		c.mu.Lock()
		fmt.Fprintf(b, "# TYPE %s counter\n", name)
		for k, v := range c.values {
			fmt.Fprintf(b, "%s%s %v\n", name, formatLabels(c.labels, c.keys[k]), v)
		}
		c.mu.Unlock()
	}
	for _, name := range sortedKeysH(r.histograms) {
		h := r.histograms[name]
		h.mu.Lock()
		fmt.Fprintf(b, "# TYPE %s histogram\n", name)
		for k, buckets := range h.counts {
			lv := h.keys[k]
			for i, bound := range h.buckets {
				fmt.Fprintf(b, "%s_bucket%s %v\n", name, withLe(h.labels, lv, fmt.Sprint(bound)), buckets[i])
			}
			fmt.Fprintf(b, "%s_bucket%s %v\n", name, withLe(h.labels, lv, "+Inf"), h.totals[k])
			fmt.Fprintf(b, "%s_sum%s %v\n", name, formatLabels(h.labels, lv), h.sums[k])
			fmt.Fprintf(b, "%s_count%s %v\n", name, formatLabels(h.labels, lv), h.totals[k])
		}
		h.mu.Unlock()
	}
	for _, name := range sortedKeysG(r.gauges) {
		g := r.gauges[name]
		g.mu.Lock()
		fmt.Fprintf(b, "# TYPE %s gauge\n", name)
		for k, v := range g.values {
			fmt.Fprintf(b, "%s%s %v\n", name, formatLabels(g.labels, g.keys[k]), v)
		}
		g.mu.Unlock()
	}
}

func withLe(names, values []string, le string) string {
	pairs := make([]string, 0, len(names)+1)
	for i, n := range names {
		v := ""
		if i < len(values) {
			v = values[i]
		}
		pairs = append(pairs, fmt.Sprintf(`%s=%q`, n, v))
	}
	return "{" + strings.Join(append(pairs, fmt.Sprintf(`le=%q`, le)), ",") + "}"
}

func sortedKeys(m map[string]*Counter) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func sortedKeysH(m map[string]*Histogram) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func sortedKeysG(m map[string]*Gauge) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
