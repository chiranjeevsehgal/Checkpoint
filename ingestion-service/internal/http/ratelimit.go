package http

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter is a per-key token bucket: each key refills rate tokens per
// second up to burst. It bounds how fast one account can create uploads so a
// single session cannot flood the API. Buckets are per-replica by design;
// use a shared store only if cross-replica abuse is ever observed.
type RateLimiter struct {
	rate  float64
	burst float64
	mu    sync.Mutex
	// tokens maps key to remaining tokens; updated maps key to last refill.
	tokens  map[string]float64
	updated map[string]time.Time
}

// NewRateLimiter builds a limiter allowing rate requests per second with the
// given burst per key. Non-positive values disable limiting (Allow always true).
func NewRateLimiter(rate, burst int) *RateLimiter {
	return &RateLimiter{
		rate:    float64(rate),
		burst:   float64(burst),
		tokens:  map[string]float64{},
		updated: map[string]time.Time{},
	}
}

// Allow takes one token for key and reports whether the request may proceed.
func (l *RateLimiter) Allow(key string) bool {
	if l.rate <= 0 || l.burst <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	tokens, ok := l.tokens[key]
	last, hasTime := l.updated[key]
	if !ok || !hasTime {
		tokens, last = l.burst, now
	}
	tokens += now.Sub(last).Seconds() * l.rate
	if tokens > l.burst {
		tokens = l.burst
	}
	if tokens < 1 {
		l.tokens[key], l.updated[key] = tokens, now
		return false
	}
	l.tokens[key], l.updated[key] = tokens-1, now
	if len(l.tokens) > 10000 {
		l.sweep(now)
	}
	return true
}

// sweep drops buckets idle longer than a full refill. Callers hold l.mu.
func (l *RateLimiter) sweep(now time.Time) {
	cutoff := now.Add(-time.Duration(l.burst/l.rate*float64(time.Second)) - time.Minute)
	for key, last := range l.updated {
		if last.Before(cutoff) {
			delete(l.tokens, key)
			delete(l.updated, key)
		}
	}
}

// RateLimit rejects over-budget requests with 429. It runs inside Auth so
// the key is the authenticated user, never an IP the service does not trust.
func RateLimit(limiter *RateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFrom(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if !limiter.Allow(principal.UserID) {
			w.Header().Set("Retry-After", "1")
			writeError(w, r, http.StatusTooManyRequests, CodeRateLimited, "Rate limit exceeded. Slow down and retry.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
