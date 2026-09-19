package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/service"
)

type fakeNotifications struct {
	channel *service.NotificationChannel
	err     error
}

func (f *fakeNotifications) Get(_ context.Context, _ string) (*service.NotificationChannel, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.channel == nil {
		return &service.NotificationChannel{}, nil
	}
	return f.channel, nil
}

func (f *fakeNotifications) Enable(_ context.Context, _ string) (*service.NotificationChannel, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.channel = &service.NotificationChannel{Enabled: true, Topic: "cp-testtopic"}
	return f.channel, nil
}

func (f *fakeNotifications) Disable(_ context.Context, _ string) error {
	if f.err != nil {
		return f.err
	}
	f.channel = nil
	return nil
}

func notificationRouter(notifications notificationService) http.Handler {
	return NewRouter(RouterDeps{
		Auth:          fakeAuthenticator{},
		Uploads:       &fakeService{},
		Devices:       &fakeDevices{owned: map[string]bool{}},
		Notifications: notifications,
		NTFPPublicURL: "http://ntfy.test:8085",
		Idem:          &fakeIdem{rows: map[string]repository.IdempotencyRecord{}},
		Metrics:       metrics.NewRegistry(),
	})
}

func TestGetNotificationsDefaultsToDisabled(t *testing.T) {
	r := notificationRouter(&fakeNotifications{})

	req := httptest.NewRequest("GET", "/v1/me/notifications", nil)
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp notificationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Enabled || resp.Topic != "" {
		t.Fatalf("expected disabled with no topic, got %+v", resp)
	}
	if resp.NtfyURL != "http://ntfy.test:8085" {
		t.Fatalf("ntfy_url: got %q", resp.NtfyURL)
	}
}

func TestEnableNotificationsReturnsTopic(t *testing.T) {
	r := notificationRouter(&fakeNotifications{})

	req := httptest.NewRequest("POST", "/v1/me/notifications", strings.NewReader(`{}`))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp notificationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Enabled || resp.Topic != "cp-testtopic" {
		t.Fatalf("enable: got %+v", resp)
	}
}

func TestDisableNotificationsClears(t *testing.T) {
	r := notificationRouter(&fakeNotifications{channel: &service.NotificationChannel{Enabled: true, Topic: "cp-testtopic"}})

	req := httptest.NewRequest("DELETE", "/v1/me/notifications", nil)
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp notificationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Enabled || resp.Topic != "" {
		t.Fatalf("disable: got %+v", resp)
	}
}

func TestNotificationsRequireAuth(t *testing.T) {
	r := notificationRouter(&fakeNotifications{})
	for _, tc := range []struct{ method, target string }{
		{"GET", "/v1/me/notifications"},
		{"POST", "/v1/me/notifications"},
		{"DELETE", "/v1/me/notifications"},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: got %d, want 401", tc.method, tc.target, rec.Code)
		}
	}
}
