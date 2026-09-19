-- +goose Up
-- Per-user ntfy credentials for per-user read authorization: when ntfy auth is
-- configured, each enabled channel is backed by a dedicated ntfy user granted
-- read-only access to exactly this topic.
ALTER TABLE user_notification_settings
    ADD COLUMN ntfy_username TEXT,
    ADD COLUMN ntfy_token    TEXT;

-- +goose Down
ALTER TABLE user_notification_settings
    DROP COLUMN IF EXISTS ntfy_token,
    DROP COLUMN IF EXISTS ntfy_username;
