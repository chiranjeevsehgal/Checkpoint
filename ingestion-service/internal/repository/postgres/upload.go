package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

// Create inserts a new UPLOADING row. The caller assigns ID, bucket,
// object key and expiry before calling.
func (p *Pool) Create(ctx context.Context, upload *domain.Upload) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, upload.UserID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO uploads (
				id, user_id, device_id, bucket, object_key,
				original_filename, content_type,
				expected_size_bytes, status, recorded_at,
				upload_url_expires_at, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
			upload.ID, upload.UserID, upload.DeviceID, upload.Bucket, upload.ObjectKey,
			upload.OriginalFilename, upload.ContentType,
			nullableInt(upload.ExpectedSize), upload.Status, nullableTime(upload.RecordedAt),
			nullableTime(upload.UploadExpiresAt), upload.CreatedAt,
		)
		return err
	})
}

// CreateUploadIdempotent claims the idempotency key and inserts the
// upload in one transaction. Concurrent same-key callers serialize on
// the (user_id, key) unique index: the winner's upload persists and
// losers receive the stored response for replay. A rollback removes
// both rows, so a failed create never blocks a legitimate retry.
func (p *Pool) CreateUploadIdempotent(ctx context.Context, params repository.IdempotentCreateParams) (*repository.IdempotentCreateResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, params.UserID); err != nil {
		return nil, err
	}

	var claimed bool
	err = tx.QueryRow(ctx, `
		INSERT INTO idempotency_keys (key, user_id, device_id, request_hash, response_status, response_body)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id, device_id, key) DO NOTHING
		RETURNING TRUE`,
		params.Key, params.UserID, params.DeviceID, params.RequestHash, params.ResponseStatus, params.ResponseBody).Scan(&claimed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var stored repository.IdempotencyRecord
			if err := tx.QueryRow(ctx, `
				SELECT key, user_id, device_id, request_hash, response_status, response_body
				FROM idempotency_keys WHERE user_id = $1 AND device_id = $2 AND key = $3`,
				params.UserID, params.DeviceID, params.Key).Scan(
				&stored.Key, &stored.UserID, &stored.DeviceID, &stored.RequestHash,
				&stored.ResponseStatus, &stored.ResponseBody); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &repository.IdempotentCreateResult{Replay: true, Stored: &stored}, nil
		}
		return nil, err
	}

	upload := params.Upload
	if _, err := tx.Exec(ctx, `
		INSERT INTO uploads (
			id, user_id, device_id, bucket, object_key,
			original_filename, content_type,
			expected_size_bytes, status, recorded_at,
			upload_url_expires_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
		upload.ID, upload.UserID, upload.DeviceID, upload.Bucket, upload.ObjectKey,
		upload.OriginalFilename, upload.ContentType,
		nullableInt(upload.ExpectedSize), upload.Status, nullableTime(upload.RecordedAt),
		nullableTime(upload.UploadExpiresAt), upload.CreatedAt,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &repository.IdempotentCreateResult{}, nil
}

// GetByIDForUser fetches one upload enforcing ownership in the query
// itself, so a missing row and a foreign row are indistinguishable.
func (p *Pool) GetByIDForUser(ctx context.Context, userID, uploadID string) (*domain.Upload, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var upload *domain.Upload
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, user_id, device_id, bucket, object_key,
				original_filename, content_type,
				expected_size_bytes, actual_size_bytes, checksum_sha256,
				status, recorded_at, upload_url_expires_at,
				uploaded_at, submitted_at, created_at, updated_at
			FROM uploads WHERE id = $1 AND user_id = $2`, uploadID, userID)
		if err != nil {
			return err
		}
		upload, err = pgx.CollectOneRow(rows, scanUpload)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return repository.ErrNotFound
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return upload, nil
}

// MarkReadyAndCreateEvent runs the service's most important transaction:
// the upload becomes READY and the transcription outbox event exists,
// atomically. Concurrent /complete calls serialize on the row lock and
// collapse to a single event through the unique (aggregate_id, event_type)
// index.
func (p *Pool) MarkReadyAndCreateEvent(ctx context.Context, params repository.CompleteParams) (*repository.CompleteResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, params.UserID); err != nil {
		return nil, err
	}

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM uploads WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		params.UploadID, params.UserID).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}

	if domain.IsCompletionIdempotent(status) {
		upload, err := getTxUpload(ctx, tx, params.UploadID, params.UserID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &repository.CompleteResult{Upload: upload, AlreadyCompleted: true}, nil
	}

	if !domain.CanTransitionToComplete(status) {
		return nil, repository.ErrInvalidState
	}

	_, err = tx.Exec(ctx, `
		UPDATE uploads SET
			status = 'READY',
			actual_size_bytes = $1,
			checksum_sha256 = NULLIF($2, ''),
			uploaded_at = $3,
			updated_at = $3
		WHERE id = $4 AND user_id = $5 AND status = 'UPLOADING'`,
		params.ActualSize, params.Checksum, params.Now, params.UploadID, params.UserID)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, aggregate_id, event_type, payload)
		VALUES ($1, $2, $3, $4) ON CONFLICT (aggregate_id, event_type) DO NOTHING`,
		params.EventID, params.UploadID, domain.EventTranscriptionRequested, params.Payload)
	if err != nil {
		return nil, err
	}

	upload, err := getTxUpload(ctx, tx, params.UploadID, params.UserID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &repository.CompleteResult{Upload: upload}, nil
}

func getTxUpload(ctx context.Context, tx pgx.Tx, uploadID, userID string) (*domain.Upload, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, user_id, device_id, bucket, object_key,
			original_filename, content_type,
			expected_size_bytes, actual_size_bytes, checksum_sha256,
			status, recorded_at, upload_url_expires_at,
			uploaded_at, submitted_at, created_at, updated_at
		FROM uploads WHERE id = $1 AND user_id = $2`, uploadID, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectOneRow(rows, scanUpload)
}

func scanUpload(row pgx.CollectableRow) (*domain.Upload, error) {
	var u domain.Upload
	var expected, actual pgtype.Int8
	var checksum pgtype.Text
	var recorded, expires, uploaded, submitted pgtype.Timestamptz
	err := row.Scan(
		&u.ID, &u.UserID, &u.DeviceID, &u.Bucket, &u.ObjectKey,
		&u.OriginalFilename, &u.ContentType,
		&expected, &actual, &checksum,
		&u.Status, &recorded, &expires,
		&uploaded, &submitted, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.ExpectedSize = intFromPg(expected)
	u.ActualSize = intFromPg(actual)
	if checksum.Valid {
		u.ChecksumSHA256 = checksum.String
	}
	u.RecordedAt = timeFromPg(recorded)
	u.UploadExpiresAt = timeFromPg(expires)
	u.UploadedAt = timeFromPg(uploaded)
	u.SubmittedAt = timeFromPg(submitted)
	return &u, nil
}

func intFromPg(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func timeFromPg(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func nullableInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return *v
}
