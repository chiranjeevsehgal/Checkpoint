package ntfy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *AdminClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewAdmin(AdminOptions{BaseURL: srv.URL + "/", Token: "tk_admin", Timeout: time.Second})
}

func TestEnsureUserSendsAdminRequest(t *testing.T) {
	var method, path, auth string
	var body map[string]string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	})

	if err := client.EnsureUser(context.Background(), "cp_user", "secret"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if method != http.MethodPut || path != "/v1/users" || auth != "Bearer tk_admin" {
		t.Fatalf("request = %s %s auth=%q", method, path, auth)
	}
	if body["username"] != "cp_user" || body["password"] != "secret" {
		t.Fatalf("body = %v", body)
	}
}

func TestEnsureUserTreatsConflictAsExisting(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})

	if err := client.EnsureUser(context.Background(), "cp_user", "secret"); err != nil {
		t.Fatalf("existing user must not error: %v", err)
	}
}

func TestGrantReadSendsAccessRequest(t *testing.T) {
	var path string
	var body map[string]string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	})

	if err := client.GrantRead(context.Background(), "cp_user", "cp-topic"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if path != "/v1/users/access" {
		t.Fatalf("path = %q", path)
	}
	if body["permission"] != "ro" || body["topic"] != "cp-topic" {
		t.Fatalf("body = %v", body)
	}
}

func TestDeleteUserTreatsNotFoundAsDone(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	if err := client.DeleteUser(context.Background(), "cp_user"); err != nil {
		t.Fatalf("missing user must not error: %v", err)
	}
}

func TestMintTokenUsesBasicAuth(t *testing.T) {
	var auth string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"token":"tk_read"}`)
	})

	token, err := client.MintToken(context.Background(), "cp_user", "secret")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if token != "tk_read" {
		t.Fatalf("token = %q", token)
	}
	if auth != basicAuth("cp_user", "secret") {
		t.Fatalf("auth = %q", auth)
	}
}

func TestAdminClientClassifiesErrors(t *testing.T) {
	cases := map[int]error{
		http.StatusUnauthorized:        ErrUnauthorized,
		http.StatusForbidden:           ErrUnauthorized,
		http.StatusInternalServerError: ErrUnavailable,
		http.StatusBadRequest:          ErrBadRequest,
	}
	for status, want := range cases {
		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		})
		if err := client.GrantRead(context.Background(), "cp_user", "cp-topic"); !errors.Is(err, want) {
			t.Fatalf("status %d: got %v, want %v", status, err, want)
		}
	}
}
