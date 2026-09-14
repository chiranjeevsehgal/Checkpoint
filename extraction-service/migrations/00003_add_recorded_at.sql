-- +goose Up
-- True recording start, propagated from transcription. NULL when the device
-- had no clock anchor; denormalized onto todos so each row carries the
-- recording time of the audio it was extracted from.
ALTER TABLE extraction_jobs ADD COLUMN recorded_at TIMESTAMPTZ;
ALTER TABLE todos ADD COLUMN recorded_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE todos DROP COLUMN IF EXISTS recorded_at;
ALTER TABLE extraction_jobs DROP COLUMN IF EXISTS recorded_at;
