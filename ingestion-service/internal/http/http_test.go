package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"checkpoint/ingestion/internal/auth"
	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/service"
)

const testUser = "11111111-1111-1111-1111-111111111111"

const testDevice = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeDevices struct {
	owned    map[string]bool
	device   *domain.Device
	claimErr error
	releaseN int
}

func (f *fakeDevices) Get(_ context.Context, _ string) (*domain.Device, error) {
	return f.device, nil
}

func (f *fakeDevices) IsOwnedBy(_ context.Context, userID, deviceID string) (bool, error) {
	return f.owned[userID+"/"+deviceID], nil
}

func (f *fakeDevices) Claim(_ context.Context, _, deviceID, _ string) (*domain.Device, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return &domain.Device{DeviceID: deviceID, State: domain.DeviceStateOwned}, nil
}

func (f *fakeDevices) Release(_ context.Context, _ string) error {
	f.releaseN++
	return nil
}

type fakeAuthenticator struct {
	authenticatedAt time.Time
}

func (a fakeAuthenticator) Authenticate(_ context.Context, token string) (Principal, error) {
	if token != "test-session" {
		return Principal{}, auth.ErrInvalidSession
	}
	at := a.authenticatedAt
	if at.IsZero() {
		at = time.Now()
	}
	return Principal{UserID: testUser, AuthenticatedAt: at}, nil
}

type fakeService struct {
	upload    *domain.Upload
	createErr error
	createN   int
	complete  func() (*domain.Upload, error)
	idem      map[string]idemEntry
}

type idemEntry struct {
	hash   string
	status int
	body   []byte
}

func (f *fakeService) CreateUpload(_ context.Context, userID string, cmd service.CreateCommand) (*service.CreateResult, error) {
	f.createN++
	if f.createErr != nil {
		return nil, f.createErr
	}
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	u := &domain.Upload{
		ID: userID + "-upload", UserID: userID, Bucket: "audio",
		ObjectKey: "audio/key", OriginalFilename: cmd.Filename,
		ContentType: cmd.ContentType, Status: domain.StatusUploading,
		CreatedAt: now, UpdatedAt: now,
	}
	return &service.CreateResult{Upload: u, UploadURL: "https://minio.test/put", ExpiresAt: now.Add(15 * time.Minute)}, nil
}

// CreateUploadIdempotent emulates the atomic key claim: the first caller
// wins and later same-key callers replay with a fresh URL body.
func (f *fakeService) CreateUploadIdempotent(_ context.Context, userID string, cmd service.CreateCommand, key, reqHash string, encode func(*service.CreateResult) []byte) (*service.IdempotentCreateOutcome, error) {
	if f.idem == nil {
		f.idem = map[string]idemEntry{}
	}
	if e, ok := f.idem[userID+"/"+key]; ok {
		stored := &repository.IdempotencyRecord{
			Key: key, UserID: userID, RequestHash: e.hash,
			ResponseStatus: e.status, ResponseBody: e.body,
		}
		// Emulate fresh-URL re-mint: same upload_id, different URL.
		fresh := append([]byte{}, e.body...)
		if len(fresh) > 0 {
			var v map[string]any
			if err := json.Unmarshal(e.body, &v); err == nil {
				if up, ok := v["upload"].(map[string]any); ok {
					up["url"] = "https://minio.test/put-fresh"
					if nb, err := json.Marshal(v); err == nil {
						fresh = nb
					}
				}
			}
		}
		return &service.IdempotentCreateOutcome{Replay: true, Stored: stored, Body: fresh}, nil
	}
	res, err := f.CreateUpload(context.Background(), userID, cmd)
	if err != nil {
		return nil, err
	}
	body := encode(res)
	f.idem[userID+"/"+key] = idemEntry{hash: reqHash, status: http.StatusCreated, body: body}
	return &service.IdempotentCreateOutcome{Result: res, Body: body}, nil
}

func (f *fakeService) CompleteUpload(_ context.Context, _, _ string, _ service.CompleteCommand) (*domain.Upload, error) {
	return f.complete()
}

func (f *fakeService) GetUpload(_ context.Context, _, _ string) (*domain.Upload, error) {
	return f.upload, nil
}

type fakeIdem struct {
	rows map[string]repository.IdempotencyRecord
}

func (f *fakeIdem) Find(_ context.Context, userID, key string) (*repository.IdempotencyRecord, error) {
	rec, ok := f.rows[userID+"/"+key]
	if !ok {
		return nil, nil
	}
	return &rec, nil
}

func (f *fakeIdem) Save(_ context.Context, rec repository.IdempotencyRecord) error {
	f.rows[rec.UserID+"/"+rec.Key] = rec
	return nil
}

func testRouter(svc *fakeService) http.Handler {
	if svc.upload == nil {
		now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
		size := int64(100)
		svc.upload = &domain.Upload{
			ID: "22222222-2222-2222-2222-222222222222", UserID: testUser,
			Bucket: "audio", ObjectKey: "audio/k",
			OriginalFilename: "m.ogg", ContentType: "audio/ogg",
			ExpectedSize: &size, Status: domain.StatusReady,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	if svc.complete == nil {
		svc.complete = func() (*domain.Upload, error) { return svc.upload, nil }
	}
	return newRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, metrics.NewRegistry())
}

func newRouter(svc *fakeService, idem repository.IdempotencyRepository, reg *metrics.Registry) http.Handler {
	return newRouterWithDevices(svc, idem, reg, &fakeDevices{
		owned: map[string]bool{testUser + "/" + testDevice: true},
	})
}

func newRouterWithDevices(svc *fakeService, idem repository.IdempotencyRepository, reg *metrics.Registry, devices deviceService) http.Handler {
	return NewRouter(RouterDeps{
		Auth:    fakeAuthenticator{},
		Uploads: svc,
		Devices: devices,
		Idem:    idem,
		Metrics: reg,
	})
}

func authed(req *http.Request) {
	req.Header.Set("Authorization", "Bearer test-session")
}

func TestUnauthorized(t *testing.T) {
	r := testRouter(&fakeService{})
	for _, tc := range []struct{ method, target string }{
		{"POST", "/v1/uploads"},
		{"POST", "/v1/uploads/22222222-2222-2222-2222-222222222222/complete"},
		{"GET", "/v1/uploads/22222222-2222-2222-2222-222222222222"},
	} {
		req := httptest.NewRequest(tc.method, tc.target, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: got %d, want 401", tc.method, tc.target, rec.Code)
		}
	}
}

func TestCreateUpload(t *testing.T) {
	svc := &fakeService{}
	r := testRouter(svc)
	body := `{"filename":"meeting.ogg","content_type":"audio/ogg","size_bytes":100,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(body))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var resp createResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != domain.StatusUploading || resp.Upload.Method != "PUT" || resp.Upload.URL == "" {
		t.Fatalf("unexpected body: %+v", resp)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID")
	}
}

func TestCreateIdempotencyReplay(t *testing.T) {
	svc := &fakeService{}
	idem := &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}
	r := newRouter(svc, idem, metrics.NewRegistry())
	body := `{"filename":"meeting.ogg","content_type":"audio/ogg","size_bytes":100,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`

	first := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(body))
	authed(req)
	req.Header.Set("Idempotency-Key", "key-1")
	r.ServeHTTP(first, req)

	second := httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(body))
	authed(req)
	req.Header.Set("Idempotency-Key", "key-1")
	r.ServeHTTP(second, req)

	var firstResp, secondResp createResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if firstResp.UploadID != secondResp.UploadID {
		t.Fatalf("replay must keep upload_id: %q vs %q", firstResp.UploadID, secondResp.UploadID)
	}
	if svc.createN != 1 {
		t.Fatalf("service executed %d times, want 1", svc.createN)
	}

	other := httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(`{"filename":"other.ogg","content_type":"audio/ogg","size_bytes":50,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	authed(req)
	req.Header.Set("Idempotency-Key", "key-1")
	r.ServeHTTP(other, req)
	if other.Code != http.StatusBadRequest {
		t.Fatalf("key reuse with different body must be 400, got %d", other.Code)
	}
}

func TestCreateIdempotencyCanonicalHash(t *testing.T) {
	svc := &fakeService{}
	r := newRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, metrics.NewRegistry())
	first := `{"filename":"meeting.ogg","content_type":"audio/ogg","size_bytes":100,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	second := "{ \"size_bytes\" : 100 , \"filename\" : \"meeting.ogg\" , \"content_type\" : \"audio/ogg\" , \"device_id\" : \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\" }"
	for i, body := range []string{first, second} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(body))
		authed(req)
		req.Header.Set("Idempotency-Key", "key-canon")
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("req %d: got %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	if svc.createN != 1 {
		t.Fatalf("canonical retry must collapse to 1 upload, got %d", svc.createN)
	}
}

func TestCreateRecordedAt(t *testing.T) {
	valid := `{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"recorded_at":"2020-01-01T00:00:00Z","device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(valid))
	authed(req)
	testRouter(&fakeService{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid recorded_at must be 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	for _, bad := range []string{
		`{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"recorded_at":"not-a-date","device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		`{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"recorded_at":"2999-01-01T00:00:00Z","device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(bad))
		authed(req)
		testRouter(&fakeService{}).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad recorded_at must be 400, got %d (%s)", rec.Code, rec.Body.String())
		}
	}
}

func TestCreateIdempotencyRecordedAtChangesHash(t *testing.T) {
	svc := &fakeService{}
	r := newRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, metrics.NewRegistry())
	first := `{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"recorded_at":"2020-01-01T00:00:00Z","device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	second := `{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"recorded_at":"2020-01-02T00:00:00Z","device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(first))
	authed(req)
	req.Header.Set("Idempotency-Key", "key-recorded")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create: got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(second))
	authed(req)
	req.Header.Set("Idempotency-Key", "key-recorded")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("different recorded_at under same key must be 400, got %d", rec.Code)
	}
}

func TestCreateBodyTooLargeAndBadKey(t *testing.T) {
	r := testRouter(&fakeService{})
	big := strings.Repeat("a", (1<<20)+10)
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(big))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize body must be 400, got %d", rec.Code)
	}

	r = testRouter(&fakeService{})
	req = httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(`{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	authed(req)
	req.Header.Set("Idempotency-Key", strings.Repeat("k", 200))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long key must be 400, got %d", rec.Code)
	}
}

func TestRequestIDSanitized(t *testing.T) {
	r := testRouter(&fakeService{})
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(`{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	authed(req)
	req.Header.Set("X-Request-ID", "bad\r\ninjected")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); strings.Contains(got, "\r") || strings.Contains(got, "\n") {
		t.Fatalf("CRLF must not be reflected: %q", got)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		makeRouter func() http.Handler
		method     string
		target     string
		body       string
		wantCode   int
		wantErr    string
	}{
		{
			name: "unsupported type",
			makeRouter: func() http.Handler {
				return testRouter(&fakeService{createErr: domain.ErrUnsupportedMediaType})
			},
			method: "POST", target: "/v1/uploads",
			body:     `{"filename":"a.ogg","content_type":"audio/ogg","size_bytes":10,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
			wantCode: http.StatusUnsupportedMediaType, wantErr: CodeUnsupportedMediaType,
		},
		{
			name: "too large",
			makeRouter: func() http.Handler {
				return testRouter(&fakeService{createErr: domain.ErrTooLarge})
			},
			method: "POST", target: "/v1/uploads",
			body:     `{"filename":"a.ogg","content_type":"audio/ogg","size_bytes":10,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
			wantCode: http.StatusRequestEntityTooLarge, wantErr: CodeUploadTooLarge,
		},
		{
			name:       "malformed json",
			makeRouter: func() http.Handler { return testRouter(&fakeService{}) },
			method:     "POST", target: "/v1/uploads",
			body:     `{bad json`,
			wantCode: http.StatusBadRequest, wantErr: CodeInvalidRequest,
		},
		{
			name: "upload not found",
			makeRouter: func() http.Handler {
				return testRouter(&fakeService{
					complete: func() (*domain.Upload, error) { return nil, repository.ErrNotFound },
				})
			},
			method: "POST", target: "/v1/uploads/22222222-2222-2222-2222-222222222222/complete",
			body:     `{}`,
			wantCode: http.StatusNotFound, wantErr: CodeUploadNotFound,
		},
		{
			name: "invalid state",
			makeRouter: func() http.Handler {
				return testRouter(&fakeService{
					complete: func() (*domain.Upload, error) { return nil, repository.ErrInvalidState },
				})
			},
			method: "POST", target: "/v1/uploads/22222222-2222-2222-2222-222222222222/complete",
			body:     `{}`,
			wantCode: http.StatusConflict, wantErr: CodeInvalidState,
		},
		{
			name: "size mismatch",
			makeRouter: func() http.Handler {
				return testRouter(&fakeService{
					complete: func() (*domain.Upload, error) { return nil, service.ErrSizeMismatch },
				})
			},
			method: "POST", target: "/v1/uploads/22222222-2222-2222-2222-222222222222/complete",
			body:     `{}`,
			wantCode: http.StatusConflict, wantErr: CodeSizeMismatch,
		},
		{
			name: "bad checksum",
			makeRouter: func() http.Handler {
				return testRouter(&fakeService{
					complete: func() (*domain.Upload, error) { return nil, domain.ErrInvalidChecksum },
				})
			},
			method: "POST", target: "/v1/uploads/22222222-2222-2222-2222-222222222222/complete",
			body:     `{}`,
			wantCode: http.StatusBadRequest, wantErr: CodeInvalidRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			authed(req)
			rec := httptest.NewRecorder()
			tc.makeRouter().ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d (%s), want %d", rec.Code, rec.Body.String(), tc.wantCode)
			}
			var env errorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if env.Error.Code != tc.wantErr {
				t.Fatalf("code: got %q, want %q", env.Error.Code, tc.wantErr)
			}
		})
	}
}

func TestCompleteAndGet(t *testing.T) {
	r := testRouter(&fakeService{})

	req := httptest.NewRequest("POST", "/v1/uploads/22222222-2222-2222-2222-222222222222/complete", strings.NewReader(`{}`))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest("POST", "/v1/uploads/not-a-uuid/complete", strings.NewReader(`{}`))
	authed(req)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: got %d, want 400", rec.Code)
	}

	req = httptest.NewRequest("GET", "/v1/uploads/22222222-2222-2222-2222-222222222222", nil)
	authed(req)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: got %d: %s", rec.Code, rec.Body.String())
	}
	var got getResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Filename != "m.ogg" || got.Status != domain.StatusReady || got.SizeBytes == nil {
		t.Fatalf("unexpected body: %+v", got)
	}
}

func TestHealth(t *testing.T) {
	r := testRouter(&fakeService{})
	for _, target := range []string{"/health/live", "/health/ready"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d", target, rec.Code)
		}
	}
}

func TestMetricsEndpoint(t *testing.T) {
	svc := &fakeService{}
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	size := int64(100)
	ready := &domain.Upload{
		ID: "22222222-2222-2222-2222-222222222222", UserID: testUser,
		Bucket: "audio", ObjectKey: "audio/k",
		OriginalFilename: "m.ogg", ContentType: "audio/ogg",
		ExpectedSize: &size, Status: domain.StatusReady,
		CreatedAt: now, UpdatedAt: now,
	}
	svc.upload = ready
	svc.complete = func() (*domain.Upload, error) { return ready, nil }
	reg := metrics.NewRegistry()
	r := newRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, reg)

	body := `{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(body))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	req = httptest.NewRequest("POST", "/v1/uploads/22222222-2222-2222-2222-222222222222/complete", strings.NewReader(`{}`))
	authed(req)
	r.ServeHTTP(httptest.NewRecorder(), req)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics: got %d", rec.Code)
	}
	out := rec.Body.String()
	for _, want := range []string{
		"uploads_created_total 1",
		`uploads_completed_total{status="READY"} 1`,
		`http_requests_total{method="POST",route="/v1/uploads",code="201"} 1`,
		`http_request_duration_seconds_bucket{route="/v1/uploads/{id}/complete"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestNormalizeRoute(t *testing.T) {
	cases := map[string]string{
		"/v1/uploads":                  "/v1/uploads",
		"/v1/uploads/abc-123":          "/v1/uploads/{id}",
		"/v1/uploads/abc-123/complete": "/v1/uploads/{id}/complete",
		"/health/live":                 "/health/live",
		"/metrics":                     "/metrics",
		"/something/else/123":          "other",
	}
	for in, want := range cases {
		if got := normalizeRoute(in); got != want {
			t.Fatalf("%s: got %q, want %q", in, got, want)
		}
	}
}

// TestCreateIdempotentPersistenceError proves a keyed create whose
// persistence fails returns 500 instead of a false 201, so the client
// retries safely.
func TestCreateIdempotentPersistenceError(t *testing.T) {
	svc := &fakeService{createErr: errors.New("db down")}
	r := newRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, metrics.NewRegistry())

	req := httptest.NewRequest("POST", "/v1/uploads",
		strings.NewReader(`{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"device_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	authed(req)
	req.Header.Set("Idempotency-Key", "key-err")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d (%s), want 500", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != CodeInternal {
		t.Fatalf("code: got %q, want %q", env.Error.Code, CodeInternal)
	}
}

func TestCreateUploadForeignDevice(t *testing.T) {
	svc := &fakeService{}
	r := newRouterWithDevices(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, metrics.NewRegistry(),
		&fakeDevices{owned: map[string]bool{}})

	body := `{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100,"device_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(body))
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign device must be 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.createN != 0 {
		t.Fatalf("foreign device must not create an upload")
	}
}

func TestDeviceRoutes(t *testing.T) {
	devices := &fakeDevices{
		owned:  map[string]bool{},
		device: &domain.Device{DeviceID: testDevice, State: domain.DeviceStateOwned},
	}
	r := newRouterWithDevices(&fakeService{}, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, metrics.NewRegistry(), devices)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/device", nil)
	authed(req)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get device: got %d (%s)", rec.Code, rec.Body.String())
	}

	claim := `{"device_id":"` + testDevice + `","cloud_claim_secret":"` + strings.Repeat("ab", 32) + `"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/device/claim", strings.NewReader(claim))
	authed(req)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim device: got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/device/release", nil)
	authed(req)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || devices.releaseN != 1 {
		t.Fatalf("release device: got %d, releases=%d", rec.Code, devices.releaseN)
	}
}

func TestDeviceReleaseRequiresRecentAuth(t *testing.T) {
	r := NewRouter(RouterDeps{
		Auth:    fakeAuthenticator{authenticatedAt: time.Now().Add(-10 * time.Minute)},
		Uploads: &fakeService{},
		Devices: &fakeDevices{owned: map[string]bool{}},
		Idem:    &fakeIdem{rows: map[string]repository.IdempotencyRecord{}},
		Metrics: metrics.NewRegistry(),
	})

	req := httptest.NewRequest("POST", "/v1/device/release", nil)
	authed(req)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stale session release must be 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != CodeReauthRequired {
		t.Fatalf("code: got %q, want %q", env.Error.Code, CodeReauthRequired)
	}
}
