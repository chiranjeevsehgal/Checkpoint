-- +goose Up
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,

    aggregate_id UUID NOT NULL,

    event_type TEXT NOT NULL,

    payload JSONB NOT NULL,

    status TEXT NOT NULL DEFAULT 'PENDING',

    attempt_count INT NOT NULL DEFAULT 0,

    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    locked_by TEXT,
    locked_until TIMESTAMPTZ,

    last_error TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ,

    CONSTRAINT outbox_status_check CHECK (
        status IN ('PENDING', 'PROCESSING', 'DELIVERED', 'FAILED')
    )
);

CREATE INDEX idx_outbox_pending
ON outbox_events(status, next_attempt_at);

-- One VAD request per upload: concurrent /complete calls collapse to a
-- single event via ON CONFLICT DO NOTHING.
CREATE UNIQUE INDEX idx_outbox_vad_upload
ON outbox_events(aggregate_id, event_type);

-- +goose Down
DROP INDEX IF EXISTS idx_outbox_vad_upload;
DROP INDEX IF EXISTS idx_outbox_pending;
DROP TABLE IF EXISTS outbox_events;
