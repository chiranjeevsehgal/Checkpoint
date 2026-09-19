-- +goose Up
-- Per-user ntfy subscriptions for reminder push notifications. ntfy_topic is
-- a server-generated bearer capability: it is unguessable and only ever
-- exposed to its owner through GET /v1/me/notifications.
CREATE TABLE user_notification_settings (
    user_id    UUID PRIMARY KEY,
    ntfy_topic TEXT,
    enabled    BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE user_notification_settings ENABLE ROW LEVEL SECURITY;

CREATE POLICY user_notification_settings_tenant ON user_notification_settings
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_request;
        GRANT SELECT, INSERT, UPDATE ON user_notification_settings TO checkpoint_request;
    END IF;

    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON user_notification_settings TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP POLICY IF EXISTS user_notification_settings_tenant ON user_notification_settings;
DROP TABLE IF EXISTS user_notification_settings;
