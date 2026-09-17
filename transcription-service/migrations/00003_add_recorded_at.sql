-- +goose Up
-- True recording start from the pendant, carried on the transcription event.
-- NULL when the device had not synced a clock before recording.
ALTER TABLE transcripts ADD COLUMN recorded_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE transcripts DROP COLUMN IF EXISTS recorded_at;
