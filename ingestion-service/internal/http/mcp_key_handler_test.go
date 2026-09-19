package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/service"
)

type fakeMcpKeyService struct {
	rows    []service.McpKey
	revoked int64
	err     error
}

func (f *fakeMcpKeyService) Create(_ context.Context, _, name string) (*service.CreatedMcpKey, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &service.CreatedMcpKey{
		McpKey: service.McpKey{ID: 3, Name: name, Prefix: "cp_mcp_abcdef", CreatedAt: time.Unix(0, 0).UTC()},
		Secret: "cp_mcp_abcdefghijklmnopqrstuvwxyz234567",
	}, nil
}

func (f *fakeMcpKeyService) List(_ context.Context, _ string) ([]service.McpKey, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeMcpKeyService) Revoke(_ context.Context, _ string, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.revoked = id
	return nil
}

func mcpKeysRouter(keys mcpKeyService) http.Handler {
	return NewRouter(RouterDeps{
		Auth:    fakeAuthenticator{},
		Uploads: &fakeService{},
		Devices: &fakeDevices{owned: map[string]bool{}},
		McpKeys: keys,
		Idem:    &fakeIdem{rows: map[string]repository.IdempotencyRecord{}},
		Metrics: metrics.NewRegistry(),
	})
}

func TestListMcpKeys(t *testing.T) {
	keys := &fakeMcpKeyService{rows: []service.McpKey{{ID: 1, Name: "laptop", Prefix: "cp_mcp_abcdef"}}}
	r := mcpKeysRouter(keys)

	req := httptest.NewRequest("GET", "/v1/me/mcp-keys", nil)
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp mcpKeyListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Keys) != 1 || resp.Keys[0].Name != "laptop" {
		t.Fatalf("unexpected keys: %+v", resp.Keys)
	}
}

func TestCreateMcpKeyReturnsSecretOnce(t *testing.T) {
	r := mcpKeysRouter(&fakeMcpKeyService{})

	req := httptest.NewRequest("POST", "/v1/me/mcp-keys", strings.NewReader(`{"name":"laptop"}`))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp createMcpKeyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.Key, "cp_mcp_") {
		t.Fatalf("missing secret: %+v", resp)
	}
	if resp.Name != "laptop" {
		t.Fatalf("name: got %q, want laptop", resp.Name)
	}
}

func TestRevokeMcpKey(t *testing.T) {
	keys := &fakeMcpKeyService{}
	r := mcpKeysRouter(keys)

	req := httptest.NewRequest("DELETE", "/v1/me/mcp-keys/42", nil)
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if keys.revoked != 42 {
		t.Fatalf("revoked id: got %d, want 42", keys.revoked)
	}
}

func TestRevokeMcpKeyRejectsBadId(t *testing.T) {
	r := mcpKeysRouter(&fakeMcpKeyService{})

	req := httptest.NewRequest("DELETE", "/v1/me/mcp-keys/abc", nil)
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestMcpKeysRequireAuth(t *testing.T) {
	r := mcpKeysRouter(&fakeMcpKeyService{})
	for _, tc := range []struct{ method, target, body string }{
		{"GET", "/v1/me/mcp-keys", ""},
		{"POST", "/v1/me/mcp-keys", `{"name":"x"}`},
		{"DELETE", "/v1/me/mcp-keys/1", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: got %d, want 401", tc.method, tc.target, rec.Code)
		}
	}
}
