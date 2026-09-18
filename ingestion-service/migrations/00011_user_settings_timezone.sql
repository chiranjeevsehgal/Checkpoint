-- +goose Up
-- Nullable: users with no timezone fall back to extraction-service's
-- reminders.timezone. Values are IANA zone ids validated by the API.
ALTER TABLE user_settings ADD COLUMN timezone TEXT;

-- +goose Down
ALTER TABLE user_settings DROP COLUMN IF EXISTS timezone;
