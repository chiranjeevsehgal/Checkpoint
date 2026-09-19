-- +goose Up
-- Per-user advance lead in seconds (5..30 minutes). NULL means unset and the
-- notification worker falls back to its configured default.
ALTER TABLE user_notification_settings ADD COLUMN advance_seconds INT;

-- +goose Down
ALTER TABLE user_notification_settings DROP COLUMN IF EXISTS advance_seconds;
