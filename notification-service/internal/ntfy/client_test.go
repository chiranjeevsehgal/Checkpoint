package ntfy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPublishSendsRequest(t *testing.T) {
	var gotPath, gotAuth, gotTitle, gotPriority, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(Options{BaseURL: srv.URL + "/", Token: "tk_secret", Timeout: time.Second, MaxBodyBytes: 4096})
	if err := client.Publish(context.Background(), "cp-topic", "Reminder", "Call Dad", "high"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if gotPath != "/cp-topic" {
		t.Fatalf("path = %q, want /cp-topic", gotPath)
	}
	if gotAuth != "Bearer tk_secret" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotTitle != "Reminder" || gotPriority != "high" {
		t.Fatalf("title/priority = %q/%q", gotTitle, gotPriority)
	}
	if gotBody != "Call Dad" {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestPublishClassifiesErrors(t *testing.T) {
	cases := map[int]error{
		http.StatusTooManyRequests:     ErrRateLimited,
		http.StatusInternalServerError: ErrServer,
		http.StatusBadRequest:          ErrBadRequest,
	}
	for status, want := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		client := New(Options{BaseURL: srv.URL, Token: "tk", Timeout: time.Second, MaxBodyBytes: 4096})
		err := client.Publish(context.Background(), "cp-topic", "", "x", "")
		srv.Close()
		if !errors.Is(err, want) {
			t.Fatalf("status %d: got %v, want %v", status, err, want)
		}
	}
}

func TestPublishTruncatesBodyOnRuneBoundary(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(Options{BaseURL: srv.URL, Token: "tk", Timeout: time.Second, MaxBodyBytes: 5})
	if err := client.Publish(context.Background(), "cp-topic", "", "héllo wörld", ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(gotBody) > 5 || !utf8.ValidString(gotBody) {
		t.Fatalf("body %q (len %d) must be <= 5 bytes and valid UTF-8", gotBody, len(gotBody))
	}
	if !strings.HasPrefix("héllo wörld", gotBody) {
		t.Fatalf("body %q is not a prefix of the input", gotBody)
	}
}
