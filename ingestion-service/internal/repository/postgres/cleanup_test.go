package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"checkpoint/ingestion/internal/domain"
)

func TestExpireStaleUploads(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	old := &domain.Upload{
		ID: uuid.NewString(), UserID: uuid.NewString(), Bucket: "audio",
		ObjectKey: "stale/" + uuid.NewString(), OriginalFilename: "old.wav",
		ContentType: "audio/wav", Status: domain.StatusUploading,
		CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour),
	}
	fresh := &domain.Upload{
		ID: uuid.NewString(), UserID: uuid.NewString(), Bucket: "audio",
		ObjectKey: "fresh/" + uuid.NewString(), OriginalFilename: "new.wav",
		ContentType: "audio/wav", Status: domain.StatusUploading,
		CreatedAt: now, UpdatedAt: now,
	}
	for _, u := range []*domain.Upload{old, fresh} {
		if err := p.Create(ctx, u); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, u := range []*domain.Upload{old, fresh} {
			_, _ = p.inner.Exec(ctx, `DELETE FROM uploads WHERE id = $1`, u.ID)
		}
	})

	stale, err := p.ExpireStaleUploads(ctx, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if len(stale) != 1 || stale[0].ObjectKey != old.ObjectKey {
		t.Fatalf("unexpected stale set: %+v", stale)
	}

	got, err := p.GetByIDForUser(ctx, old.UserID, old.ID)
	if err != nil || got.Status != domain.StatusExpired {
		t.Fatalf("old status: %+v %v", got, err)
	}
	got, err = p.GetByIDForUser(ctx, fresh.UserID, fresh.ID)
	if err != nil || got.Status != domain.StatusUploading {
		t.Fatalf("fresh status: %+v %v", got, err)
	}
}
