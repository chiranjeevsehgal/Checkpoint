package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient(baseURL string) *Client {
	return New(Options{
		BaseURL:             baseURL,
		APIKey:              "gsk_test",
		Model:               "openai/gpt-oss-120b",
		MaxCompletionTokens: 2048,
		Timeout:             5 * time.Second,
	})
}

func TestChatReturnsAssistantContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gsk_test" {
			t.Errorf("missing bearer token")
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"A short recap.","reasoning":"thinking..."}}]}`))
	}))
	defer srv.Close()

	content, err := testClient(srv.URL).Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if content != "A short recap." {
		t.Fatalf("content wrong: %q", content)
	}
}

func TestChatClassifiesErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
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

func TestChatSendsModelAndTuning(t *testing.T) {
	var payload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	client := New(Options{
		BaseURL: srv.URL, APIKey: "gsk_test", Model: "openai/gpt-oss-120b",
		MaxCompletionTokens: 2048, Temperature: 0.4, TopP: 1,
		ReasoningEffort: "low", Timeout: 5 * time.Second,
	})
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if payload["model"] != "openai/gpt-oss-120b" {
		t.Fatalf("model not sent: %+v", payload)
	}
	if payload["top_p"] != float64(1) {
		t.Fatalf("top_p not sent: %+v", payload)
	}
	if payload["reasoning_effort"] != "low" {
		t.Fatalf("reasoning_effort not sent: %+v", payload)
	}
	if _, ok := payload["response_format"]; ok {
		t.Fatalf("narrative summaries must not force json_object: %+v", payload)
	}
}

func TestChatRetriesRateLimitThenSucceeds(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"slow down"}}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}]}`))
	}))
	defer srv.Close()

	client := New(Options{
		BaseURL: srv.URL, APIKey: "gsk_test", Model: "openai/gpt-oss-120b",
		MaxCompletionTokens: 2048, MaxAttempts: 3, Timeout: 5 * time.Second,
	})
	content, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if content != "recovered" {
		t.Fatalf("content = %q, want recovered", content)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestChatDoesNotRetryBadRequest(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"context length exceeded"}}`))
	}))
	defer srv.Close()

	client := New(Options{
		BaseURL: srv.URL, APIKey: "gsk_test", Model: "openai/gpt-oss-120b",
		MaxCompletionTokens: 2048, MaxAttempts: 3, Timeout: 5 * time.Second,
	})
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (must not retry)", calls)
	}
}

func TestChatCanceledContextIsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := New(Options{
		BaseURL: srv.URL, APIKey: "gsk_test", Model: "openai/gpt-oss-120b",
		MaxCompletionTokens: 2048, MaxAttempts: 3, Timeout: 5 * time.Second,
	})
	if _, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}
