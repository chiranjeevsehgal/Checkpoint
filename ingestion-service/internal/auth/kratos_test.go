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

func TestKratosAuthenticateEmailVerification(t *testing.T) {
	const identityID = "11111111-1111-1111-1111-111111111111"
	body := `{
		"active": true,
		"authenticated_at": "2026-09-06T00:00:00Z",
		"identity": {
			"id": "` + identityID + `",
			"verifiable_addresses": [
				{"via":"email","verified":true,"status":"completed","verified_at":"2026-09-06T00:00:00Z"},
				{"via":"email","verified":true,"status":"completed","verified_at":"2026-09-07T12:30:00Z"},
				{"via":"sms","verified":true,"status":"completed","verified_at":"2026-09-08T00:00:00Z"},
				{"via":"email","verified":false,"status":"sent","verified_at":"2026-09-09T00:00:00Z"},
				{"via":"email","verified":true,"status":"completed","verified_at":"not-a-time"}
			]
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	authn := NewKratosAuthenticator(server.URL, time.Second)
	principal, err := authn.Authenticate(context.Background(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 9, 7, 12, 30, 0, 0, time.UTC)
	if !principal.EmailVerifiedAt.Equal(want) {
		t.Fatalf("email verified at: got %v, want %v", principal.EmailVerifiedAt, want)
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

func TestKratosAdminListIdentities(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/identities" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		queries = append(queries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page_token") == "" {
			w.Header().Set("Link", `<http://kratos:4434/admin/identities?page_size=250&page_token=next-token>; rel="next"`)
			_, _ = w.Write([]byte(`[{"id":"a","created_at":"2026-09-15T10:00:00Z","verifiable_addresses":[{"via":"email","verified":false,"status":"sent"}]}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"b","created_at":"2026-09-15T11:00:00Z","verifiable_addresses":[{"via":"email","verified":true,"status":"completed"}]}]`))
	}))
	defer server.Close()

	admin := NewKratosAdmin(server.URL, time.Second)
	first, next, err := admin.ListIdentities(context.Background(), 250, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first) != 1 || first[0].ID != "a" || HasVerifiedEmail(first[0].VerifiableAddresses) {
		t.Fatalf("first page = %+v", first)
	}
	if next != "next-token" {
		t.Fatalf("next token = %q, want next-token", next)
	}

	second, nextAgain, err := admin.ListIdentities(context.Background(), 250, next)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second) != 1 || second[0].ID != "b" || !HasVerifiedEmail(second[0].VerifiableAddresses) {
		t.Fatalf("second page = %+v", second)
	}
	if nextAgain != "" {
		t.Fatalf("next token = %q, want empty", nextAgain)
	}
	if len(queries) != 2 || !strings.Contains(queries[1], "page_token=next-token") {
		t.Fatalf("queries = %v, want page_token forwarded", queries)
	}
}

func TestKratosAdminListIdentitiesProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	admin := NewKratosAdmin(server.URL, time.Second)
	if _, _, err := admin.ListIdentities(context.Background(), 250, ""); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("got %v, want ErrProviderUnavailable", err)
	}
}

func TestNextPageToken(t *testing.T) {
	if got := nextPageToken(`<http://kratos/admin/identities?page_size=250&page_token=abc>; rel="next"`); got != "abc" {
		t.Fatalf("token = %q, want abc", got)
	}
	if got := nextPageToken(`<http://kratos/admin/identities?page_token=abc>; rel="prev"`); got != "" {
		t.Fatalf("token = %q, want empty", got)
	}
	if got := nextPageToken(""); got != "" {
		t.Fatalf("token = %q, want empty", got)
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
