package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/service"
)

type fakeSettings struct {
	languages     []string
	timezone      string
	getErr        error
	setErr        error
	saved         []string
	savedTimezone string
}

func (f *fakeSettings) Get(_ context.Context, _ string) (*service.Settings, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &service.Settings{Languages: f.languages, Timezone: f.timezone, Available: domain.SupportedLanguages}, nil
}

func (f *fakeSettings) SetLanguages(_ context.Context, _ string, codes []string) ([]string, error) {
	if f.setErr != nil {
		return nil, f.setErr
	}
	normalized, err := domain.ValidateLanguages(codes)
	if err != nil {
		return nil, err
	}
	f.saved = normalized
	f.languages = normalized
	return normalized, nil
}

func (f *fakeSettings) SetTimezone(_ context.Context, _, timezone string) (string, error) {
	if f.setErr != nil {
		return "", f.setErr
	}
	normalized, err := domain.ValidateTimezone(timezone)
	if err != nil {
		return "", err
	}
	f.savedTimezone = normalized
	f.timezone = normalized
	return normalized, nil
}

func settingsRouter(settings settingsService) http.Handler {
	return NewRouter(RouterDeps{
		Auth:     fakeAuthenticator{},
		Uploads:  &fakeService{},
		Devices:  &fakeDevices{owned: map[string]bool{}},
		Settings: settings,
		Idem:     &fakeIdem{rows: map[string]repository.IdempotencyRecord{}},
		Metrics:  metrics.NewRegistry(),
	})
}

func TestGetSettingsReturnsSelectionAndCatalog(t *testing.T) {
	r := settingsRouter(&fakeSettings{languages: []string{"eng"}, timezone: "Europe/Berlin"})

	req := httptest.NewRequest("GET", "/v1/me/settings", nil)
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp settingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Languages) != 1 || resp.Languages[0] != "eng" {
		t.Fatalf("languages: got %v, want [eng]", resp.Languages)
	}
	if resp.Timezone != "Europe/Berlin" {
		t.Fatalf("timezone: got %q, want Europe/Berlin", resp.Timezone)
	}
	if len(resp.Available) != len(domain.SupportedLanguages) {
		t.Fatalf("available: got %d, want %d", len(resp.Available), len(domain.SupportedLanguages))
	}
}

func TestPutSettingsNormalizesAndPersists(t *testing.T) {
	fake := &fakeSettings{}
	r := settingsRouter(fake)

	req := httptest.NewRequest("PUT", "/v1/me/settings", strings.NewReader(`{"languages":[" ENG ","hin","eng"]}`))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp updateSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Languages) != 2 || resp.Languages[0] != "eng" || resp.Languages[1] != "hin" {
		t.Fatalf("languages: got %v, want [eng hin]", resp.Languages)
	}
	if len(fake.saved) != 2 || fake.saved[0] != "eng" {
		t.Fatalf("persisted: got %v", fake.saved)
	}
}

func TestPutSettingsPersistsTimezoneWithoutTouchingLanguages(t *testing.T) {
	fake := &fakeSettings{languages: []string{"eng"}}
	r := settingsRouter(fake)

	req := httptest.NewRequest("PUT", "/v1/me/settings", strings.NewReader(`{"timezone":" Europe/Berlin "}`))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp updateSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Timezone != "Europe/Berlin" {
		t.Fatalf("timezone: got %q, want Europe/Berlin", resp.Timezone)
	}
	if len(resp.Languages) != 1 || resp.Languages[0] != "eng" {
		t.Fatalf("languages clobbered: got %v", resp.Languages)
	}
	if len(fake.saved) != 0 {
		t.Fatalf("timezone PUT must not rewrite languages: %v", fake.saved)
	}
}

func TestPutSettingsRejectsInvalidTimezone(t *testing.T) {
	r := settingsRouter(&fakeSettings{})

	req := httptest.NewRequest("PUT", "/v1/me/settings", strings.NewReader(`{"timezone":"Not/AZone"}`))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestPutSettingsRejectsUnknownLanguage(t *testing.T) {
	r := settingsRouter(&fakeSettings{})

	req := httptest.NewRequest("PUT", "/v1/me/settings", strings.NewReader(`{"languages":["zzz"]}`))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != CodeInvalidRequest {
		t.Fatalf("code: got %q, want %q", env.Error.Code, CodeInvalidRequest)
	}
}

func TestSettingsRequiresAuth(t *testing.T) {
	r := settingsRouter(&fakeSettings{})
	for _, tc := range []struct{ method, target string }{
		{"GET", "/v1/me/settings"},
		{"PUT", "/v1/me/settings"},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: got %d, want 401", tc.method, tc.target, rec.Code)
		}
	}
}
