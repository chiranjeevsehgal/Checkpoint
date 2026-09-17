package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient(baseURL string) *Client {
	return New(baseURL, "gsk_test", "openai/gpt-oss-120b", 8192, 0, 5*time.Second)
}

func TestChatReturnsAssistantContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gsk_test" {
			t.Errorf("missing bearer token")
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"todos\":[]}","reasoning":"thinking..."}}]}`))
	}))
	defer srv.Close()

	content, err := testClient(srv.URL).Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if content != `{"todos":[]}` {
		t.Fatalf("content wrong: %q", content)
	}
}

func TestChatClassifiesErrors(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		want    error
	}{
		{http.StatusTooManyRequests, `{"error":{"message":"rate limit"}}`, ErrRateLimited},
		{http.StatusInternalServerError, `{"error":{"message":"upstream"}}`, ErrServer},
		{http.StatusBadRequest, `{"error":{"message":"context length exceeded"}}`, ErrBadRequest},
		{http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, ErrBadRequest},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		_, err := testClient(srv.URL).Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
		srv.Close()
		if !errors.Is(err, c.want) {
			t.Fatalf("status %d: want %v, got %v", c.status, c.want, err)
		}
	}
}

func TestChatRejectsEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"","reasoning":"only reasoning"}}]}`))
	}))
	defer srv.Close()

	if _, err := testClient(srv.URL).Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); !errors.Is(err, ErrServer) {
		t.Fatalf("expected ErrServer for empty content, got %v", err)
	}
}
