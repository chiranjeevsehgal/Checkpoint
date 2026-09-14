-- +goose Up
CREATE TABLE account_deletions (
    user_id UUID PRIMARY KEY,

    status TEXT NOT NULL DEFAULT 'PENDING',
    attempt_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,

    CONSTRAINT account_deletions_status_check CHECK (status IN ('PENDING', 'PROCESSING', 'COMPLETE'))
);

CREATE INDEX idx_account_deletions_due
ON account_deletions(status, next_attempt_at);

ALTER TABLE account_deletions ENABLE ROW LEVEL SECURITY;

CREATE POLICY account_deletions_tenant ON account_deletions
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_request;
        GRANT SELECT, INSERT ON account_deletions TO checkpoint_request;
    END IF;

    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON account_deletions TO checkpoint_worker;
    END IF;
END
$$;

-- +goose Down
DROP INDEX IF EXISTS idx_account_deletions_due;
DROP POLICY IF EXISTS account_deletions_tenant ON account_deletions;
DROP TABLE IF EXISTS account_deletions;
