package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"checkpoint/ingestion/internal/auth"
)

func TestRateLimiterAllowsBurstThenDenies(t *testing.T) {
	limiter := NewRateLimiter(1000, 3)
	for i := 0; i < 3; i++ {
		if !limiter.Allow("user-1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if limiter.Allow("user-1") {
		t.Fatal("request over burst should be denied")
	}
}

func TestRateLimiterKeysAreIndependent(t *testing.T) {
	limiter := NewRateLimiter(1000, 1)
	if !limiter.Allow("a") || !limiter.Allow("b") {
		t.Fatal("first request per key should be allowed")
	}
	if limiter.Allow("a") {
		t.Fatal("second request for a should be denied")
	}
}

func TestRateLimiterDisabledOnNonPositive(t *testing.T) {
	limiter := NewRateLimiter(0, 0)
	for i := 0; i < 100; i++ {
		if !limiter.Allow("anyone") {
			t.Fatal("disabled limiter should allow everything")
		}
	}
}

func TestRateLimitMiddlewareResponds429(t *testing.T) {
	limiter := NewRateLimiter(1000, 1)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RateLimit(limiter, next)

	principal := auth.Principal{UserID: "user-9"}
	limited := false
	for i := 0; i < 3 && !limited; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/uploads", nil)
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, principal))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("expected a 429 once the burst is exhausted")
	}
}

func TestRateLimitMiddlewareSkipsUnauthenticated(t *testing.T) {
	limiter := NewRateLimiter(1000, 1)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", nil)
	rec := httptest.NewRecorder()
	RateLimit(limiter, next).ServeHTTP(rec, req)
	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("requests without a principal must pass through, got %d", rec.Code)
	}
}
