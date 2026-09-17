-- +goose Up
CREATE TABLE user_settings (
    user_id    UUID PRIMARY KEY,
    languages  TEXT[] NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE user_settings ENABLE ROW LEVEL SECURITY;

CREATE POLICY user_settings_tenant ON user_settings
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_request;
        GRANT SELECT, INSERT, UPDATE ON user_settings TO checkpoint_request;
    END IF;

    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON user_settings TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP POLICY IF EXISTS user_settings_tenant ON user_settings;
DROP TABLE IF EXISTS user_settings;
