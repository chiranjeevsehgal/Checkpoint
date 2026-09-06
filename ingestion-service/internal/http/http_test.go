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

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/metrics"
	"checkpoint/ingestion/internal/repository"
	"checkpoint/ingestion/internal/service"
)

const testUser = "11111111-1111-1111-1111-111111111111"

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
	return NewRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, nil, nil, metrics.NewRegistry())
}

func authed(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+testUser)
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
	body := `{"filename":"meeting.ogg","content_type":"audio/ogg","size_bytes":100}`
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
	r := NewRouter(svc, idem, nil, nil, metrics.NewRegistry())
	body := `{"filename":"meeting.ogg","content_type":"audio/ogg","size_bytes":100}`

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
	req = httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(`{"filename":"other.ogg","content_type":"audio/ogg","size_bytes":50}`))
	authed(req)
	req.Header.Set("Idempotency-Key", "key-1")
	r.ServeHTTP(other, req)
	if other.Code != http.StatusBadRequest {
		t.Fatalf("key reuse with different body must be 400, got %d", other.Code)
	}
}

func TestCreateIdempotencyCanonicalHash(t *testing.T) {
	svc := &fakeService{}
	r := NewRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, nil, nil, metrics.NewRegistry())
	first := `{"filename":"meeting.ogg","content_type":"audio/ogg","size_bytes":100}`
	second := "{ \"size_bytes\" : 100 , \"filename\" : \"meeting.ogg\" , \"content_type\" : \"audio/ogg\" }"
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
	req = httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(`{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100}`))
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
	req := httptest.NewRequest("POST", "/v1/uploads", strings.NewReader(`{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100}`))
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
			body:     `{"filename":"a.ogg","content_type":"audio/ogg","size_bytes":10}`,
			wantCode: http.StatusUnsupportedMediaType, wantErr: CodeUnsupportedMediaType,
		},
		{
			name: "too large",
			makeRouter: func() http.Handler {
				return testRouter(&fakeService{createErr: domain.ErrTooLarge})
			},
			method: "POST", target: "/v1/uploads",
			body:     `{"filename":"a.ogg","content_type":"audio/ogg","size_bytes":10}`,
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
	r := NewRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, nil, nil, reg)

	body := `{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100}`
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
	r := NewRouter(svc, &fakeIdem{rows: map[string]repository.IdempotencyRecord{}}, nil, nil, metrics.NewRegistry())

	req := httptest.NewRequest("POST", "/v1/uploads",
		strings.NewReader(`{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":100}`))
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
