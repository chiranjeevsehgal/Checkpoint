package postgres

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

func idempotentParams(userID, key string) repository.IdempotentCreateParams {
	now := time.Now().UTC().Truncate(time.Millisecond)
	size := int64(100)
	return repository.IdempotentCreateParams{
		Upload: &domain.Upload{
			ID: uuid.NewString(), UserID: userID, Bucket: "audio",
			ObjectKey: "u/2026/09/06/" + uuid.NewString(), OriginalFilename: "m.ogg",
			ContentType: "audio/ogg", ExpectedSize: &size,
			Status: domain.StatusUploading, CreatedAt: now, UpdatedAt: now,
		},
		Key:            key,
		UserID:         userID,
		RequestHash:    "hash-1",
		ResponseStatus: 201,
		ResponseBody:   []byte(`{"upload_id":"winner"}`),
	}
}

// TestConcurrentCreateUploadIdempotentSingleRow proves the fix for the
// check-then-act race: concurrent same-key creates collapse to one
// upload row and every caller replays the winner's response.
func TestConcurrentCreateUploadIdempotentSingleRow(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	userID := uuid.NewString()
	key := "key-" + uuid.NewString()
	const n = 8

	// Registered first so rows are removed even when an assertion fails.
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = p.inner.Exec(ctx, `DELETE FROM uploads WHERE user_id = $1`, userID)
		_, _ = p.inner.Exec(ctx, `DELETE FROM idempotency_keys WHERE user_id = $1`, userID)
	})

	results := make([]*repository.IdempotentCreateResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = p.CreateUploadIdempotent(ctx, idempotentParams(userID, key))
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent create: %v", err)
		}
	}

	fresh := 0
	for _, res := range results {
		if res.Replay {
			// JSONB storage normalizes whitespace, so compare the
			// replayed response semantically, not byte-for-byte.
			if res.Stored == nil || replayedUploadID(t, res.Stored.ResponseBody) != "winner" {
				t.Fatalf("replay must carry the winner response, got %+v", res.Stored)
			}
		} else {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("want exactly 1 winner, got %d", fresh)
	}

	var uploads int
	if err := p.inner.QueryRow(ctx,
		`SELECT COUNT(*) FROM uploads WHERE user_id = $1`, userID).Scan(&uploads); err != nil {
		t.Fatalf("count: %v", err)
	}
	if uploads != 1 {
		t.Fatalf("want 1 upload row, got %d", uploads)
	}

	var storedBody []byte
	if err := p.inner.QueryRow(ctx,
		`SELECT response_body FROM idempotency_keys WHERE user_id = $1 AND key = $2`,
		userID, key).Scan(&storedBody); err != nil {
		t.Fatalf("stored key: %v", err)
	}
	if replayedUploadID(t, storedBody) != "winner" {
		t.Fatalf("unexpected stored body: %s", storedBody)
	}
}

// replayedUploadID decodes a stored idempotency response body.
func replayedUploadID(t *testing.T, body []byte) string {
	t.Helper()
	var decoded struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode stored body: %v", err)
	}
	return decoded.UploadID
}

// TestCreateUploadIdempotentRollbackLeavesNoClaim proves a failed upload
// insert rolls back the key claim too, so a legitimate retry is not
// blocked by a phantom replay.
func TestCreateUploadIdempotentRollbackLeavesNoClaim(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	userID := uuid.NewString()
	key := "key-" + uuid.NewString()

	bad := idempotentParams(userID, key)
	bad.Upload.ID = "not-a-uuid"
	if _, err := p.CreateUploadIdempotent(ctx, bad); err == nil {
		t.Fatal("invalid upload must fail the transaction")
	}

	var keys int
	if err := p.inner.QueryRow(ctx,
		`SELECT COUNT(*) FROM idempotency_keys WHERE user_id = $1 AND key = $2`,
		userID, key).Scan(&keys); err != nil {
		t.Fatalf("count: %v", err)
	}
	if keys != 0 {
		t.Fatal("rolled-back claim must not block a retry")
	}
}
