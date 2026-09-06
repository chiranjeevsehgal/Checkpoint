package vadclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmitJobAccepted(t *testing.T) {
	var got jobPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/jobs" {
			t.Errorf("path: %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	err := New(srv.URL).SubmitJob(context.Background(), JobRequest{
		EventID: "e1", AudioID: "a1", Bucket: "audio",
		ObjectKey: "u/2026/09/06/a1", ContentType: "audio/wav", SizeBytes: 100,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got.EventID != "e1" || got.Object.Bucket != "audio" || got.SizeBytes != 100 {
		t.Fatalf("unexpected payload: %+v", got)
	}
}

func TestSubmitJobRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := New(srv.URL).SubmitJob(context.Background(), JobRequest{}); err == nil {
		t.Fatal("500 must be an error for the dispatcher to retry")
	}
}
