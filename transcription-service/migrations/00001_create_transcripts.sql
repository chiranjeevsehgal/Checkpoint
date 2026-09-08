-- +goose Up
CREATE TABLE transcripts (
    audio_id          TEXT PRIMARY KEY,
    user_id           TEXT,
    text              TEXT NOT NULL,
    language          TEXT,
    duration_seconds  DOUBLE PRECISION,
    speaker_segments  JSONB,
    provider          TEXT,
    request_id        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE transcripts;