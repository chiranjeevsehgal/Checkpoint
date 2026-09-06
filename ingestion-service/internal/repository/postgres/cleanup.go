package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chiranjeevsehgal/Checkpoint-1.0.0/ingestion-service/internal/repository"
)

// ExpireStaleUploads marks abandoned UPLOADING rows EXPIRED and returns
// their object locations for best-effort storage cleanup.
func (p *Pool) ExpireStaleUploads(ctx context.Context, olderThan, now time.Time) ([]repository.ExpiredUpload, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := p.inner.Query(ctx, `
		UPDATE uploads SET status = 'EXPIRED', updated_at = $1
		WHERE status = 'UPLOADING' AND created_at < $2
		RETURNING bucket, object_key`, now, olderThan)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (repository.ExpiredUpload, error) {
		var e repository.ExpiredUpload
		err := row.Scan(&e.Bucket, &e.ObjectKey)
		return e, err
	})
}
