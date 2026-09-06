-- +goose Up
CREATE TABLE uploads (
    id UUID PRIMARY KEY,

    user_id UUID NOT NULL,

    bucket TEXT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,

    original_filename TEXT NOT NULL,
    content_type TEXT NOT NULL,

    expected_size_bytes BIGINT,
    actual_size_bytes BIGINT,

    checksum_sha256 TEXT,

    status TEXT NOT NULL,

    upload_url_expires_at TIMESTAMPTZ,

    uploaded_at TIMESTAMPTZ,
    submitted_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uploads_status_check CHECK (
        status IN (
            'UPLOADING',
            'READY',
            'SUBMITTED',
            'EXPIRED',
            'REJECTED'
        )
    )
);

CREATE INDEX idx_uploads_user_created
ON uploads(user_id, created_at DESC);

CREATE INDEX idx_uploads_status_created
ON uploads(status, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_uploads_status_created;
DROP INDEX IF EXISTS idx_uploads_user_created;
DROP TABLE IF EXISTS uploads;
