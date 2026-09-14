package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKratosAuthenticate(t *testing.T) {
	const identityID = "11111111-1111-1111-1111-111111111111"

	sessionBody := func(active bool, verified bool) string {
		return `{
			"active": ` + boolText(active) + `,
			"authenticated_at": "2026-09-06T00:00:00Z",
			"identity": {
				"id": "` + identityID + `",
				"verifiable_addresses": [{"via":"email","verified":` + boolText(verified) + `,"status":"completed"}]
			}
		}`
	}

	cases := []struct {
		name       string
		status     int
		body       string
		wantErr    error
		wantUserID string
	}{
		{name: "valid verified session", status: http.StatusOK, body: sessionBody(true, true), wantUserID: identityID},
		{name: "inactive session", status: http.StatusOK, body: sessionBody(false, true), wantErr: ErrInvalidSession},
		{name: "unverified identity", status: http.StatusOK, body: sessionBody(true, false), wantErr: ErrVerificationRequired},
		{name: "kratos 401", status: http.StatusUnauthorized, body: `{}`, wantErr: ErrInvalidSession},
		{name: "kratos 403", status: http.StatusForbidden, body: `{}`, wantErr: ErrInvalidSession},
		{name: "kratos 500", status: http.StatusInternalServerError, body: `{}`, wantErr: ErrProviderUnavailable},
		{name: "kratos 429", status: http.StatusTooManyRequests, body: `{}`, wantErr: ErrProviderUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/sessions/whoami" {
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
				if r.Header.Get("X-Session-Token") != "token" {
					t.Fatalf("missing session token header")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			authn := NewKratosAuthenticator(server.URL, time.Second)
			principal, err := authn.Authenticate(context.Background(), "token")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got error %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if principal.UserID != tc.wantUserID {
				t.Fatalf("user id: got %q, want %q", principal.UserID, tc.wantUserID)
			}
		})
	}
}

func TestKratosAuthenticateNetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	authn := NewKratosAuthenticator(url, 200*time.Millisecond)
	if _, err := authn.Authenticate(context.Background(), "token"); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("got %v, want ErrProviderUnavailable", err)
	}
}

func TestKratosAdminRevokeOtherSessions(t *testing.T) {
	const identityID = "11111111-1111-1111-1111-111111111111"
	disabled := map[string]bool{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/identities/"+identityID+"/sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"keep"},{"id":"other-1"},{"id":"other-2"}]`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/admin/sessions/"):
			disabled[strings.TrimPrefix(r.URL.Path, "/admin/sessions/")] = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	admin := NewKratosAdmin(server.URL, time.Second)
	if err := admin.RevokeOtherSessions(context.Background(), identityID, "keep"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disabled["keep"] || !disabled["other-1"] || !disabled["other-2"] {
		t.Fatalf("disabled sessions = %v", disabled)
	}
}

func TestKratosAdminRevokeOtherSessionsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	admin := NewKratosAdmin(server.URL, time.Second)
	if err := admin.RevokeOtherSessions(context.Background(), "id", "keep"); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("got %v, want ErrProviderUnavailable", err)
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
