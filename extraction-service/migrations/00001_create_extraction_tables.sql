-- +goose Up
-- extraction_jobs is the durable batch queue: the consumer inserts one
-- pending row per (user, audio, extraction_type); the batcher claims
-- batches of N rows per user with FOR UPDATE SKIP LOCKED. Exactly-N
-- batching survives restarts because the "queue" is data, not memory.
CREATE TABLE extraction_jobs (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         TEXT NOT NULL,
    audio_id        TEXT NOT NULL,
    extraction_type TEXT NOT NULL DEFAULT 'todo',
    text            TEXT NOT NULL,
    language        TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',   -- pending|processing|done|failed
    attempts        INT NOT NULL DEFAULT 0,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at    TIMESTAMPTZ,
    UNIQUE (user_id, audio_id, extraction_type)
);

-- Drives the ready-users GROUP BY and the per-user claims.
CREATE INDEX extraction_jobs_pending_idx
    ON extraction_jobs (user_id, status, created_at)
    WHERE status = 'pending';

-- Todos extracted per audio, replaced atomically on each successful run
-- (DELETE + INSERT in one transaction) so redeliveries and re-runs can
-- neither duplicate nor leave stale rows.
CREATE TABLE todos (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     TEXT NOT NULL,
    audio_id    TEXT NOT NULL,
    text        TEXT NOT NULL,
    model       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id, text)
);

CREATE INDEX todos_user_id_idx ON todos (user_id);
CREATE INDEX todos_audio_id_idx ON todos (audio_id);

-- +goose Down
DROP TABLE todos;
DROP TABLE extraction_jobs;
