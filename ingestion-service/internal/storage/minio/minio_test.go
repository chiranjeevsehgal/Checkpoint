package minio

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	minioapi "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"checkpoint/ingestion/internal/storage"
)

func testStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	endpoint := os.Getenv("TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_MINIO_ENDPOINT not set, skipping integration test")
	}
	ctx := context.Background()
	s, err := New(ctx,
		endpoint,
		os.Getenv("TEST_MINIO_ACCESS_KEY"),
		os.Getenv("TEST_MINIO_SECRET_KEY"),
		os.Getenv("TEST_MINIO_BUCKET"),
		false,
		endpoint,
	)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return s, os.Getenv("TEST_MINIO_BUCKET")
}

func TestPresignPutStatDelete(t *testing.T) {
	s, bucket := testStorage(t)
	ctx := context.Background()
	key := "audio/test/2026/09/06/" + uuid.NewString()
	body := []byte("fake-audio-bytes")

	putURL, err := s.CreateUploadURL(ctx, bucket, key, "audio/wav", 15*time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if putURL == "" {
		t.Fatal("empty presigned URL")
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "audio/wav")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("put status: %s", resp.Status)
	}

	info, err := s.StatObject(ctx, bucket, key)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size != int64(len(body)) {
		t.Fatalf("size: got %d, want %d", info.Size, len(body))
	}

	if err := s.DeleteObject(ctx, bucket, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.StatObject(ctx, bucket, key); err != storage.ErrObjectNotFound {
		t.Fatalf("stat after delete must be ErrObjectNotFound, got %v", err)
	}
}

func TestPing(t *testing.T) {
	s, _ := testStorage(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

// TestPresignOfflineWithUnreachableHosts guards the split-horizon setup:
// presigning must be pure crypto and never dial the network, because the
// public endpoint is unreachable from inside the API container.
func TestPresignOfflineWithUnreachableHosts(t *testing.T) {
	mk := func(endpoint string) *minioapi.Client {
		c, err := minioapi.New(endpoint, &minioapi.Options{
			Creds:  credentials.NewStaticV4("user", "password", ""),
			Secure: false,
			Region: defaultRegion,
		})
		if err != nil {
			t.Fatalf("client: %v", err)
		}
		return c
	}
	s := &Storage{client: mk("internal.invalid:9000"), public: mk("public.invalid:9000")}

	url, err := s.CreateUploadURL(context.Background(), "audio", "u/k", "audio/wav", 15*time.Minute)
	if err != nil {
		t.Fatalf("offline presign must not dial: %v", err)
	}
	if !strings.Contains(url, "public.invalid:9000") {
		t.Fatalf("presigned URL must use the public host, got %s", url)
	}
}
