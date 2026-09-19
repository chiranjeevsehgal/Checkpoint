-- +goose Up
-- rollup-service scans transcripts by recording activity; this functional
-- index keeps the nightly active-user query off a full table scan.
CREATE INDEX IF NOT EXISTS idx_transcripts_activity
    ON transcripts ((coalesce(recorded_at, created_at)));

-- +goose Down
DROP INDEX IF EXISTS idx_transcripts_activity;
