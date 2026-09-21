-- +goose Up
-- Raw trigger event, kept so a terminally failed job can be handed to the
-- central retry service. Cleared on terminal success to avoid duplicating
-- the transcript text that extraction_jobs.text already holds.
ALTER TABLE extraction_jobs ADD COLUMN source_event BYTEA;

-- +goose Down
ALTER TABLE extraction_jobs DROP COLUMN IF EXISTS source_event;
