-- +goose Up
-- Device-resolved recording start (pendant phone-time anchor). NULL when the
-- device had not synced a clock before recording, or for older clients.
ALTER TABLE uploads ADD COLUMN recorded_at TIMESTAMPTZ;

CREATE INDEX idx_uploads_user_recorded
ON uploads(user_id, recorded_at DESC)
WHERE recorded_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_uploads_user_recorded;
ALTER TABLE uploads DROP COLUMN IF EXISTS recorded_at;
