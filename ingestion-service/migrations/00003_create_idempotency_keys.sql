-- +goose Up
CREATE TABLE idempotency_keys (
    key TEXT NOT NULL,
    user_id UUID NOT NULL,

    request_hash TEXT NOT NULL,

    response_status INT NOT NULL,
    response_body JSONB NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, key)
);

-- +goose Down
DROP TABLE IF EXISTS idempotency_keys;
