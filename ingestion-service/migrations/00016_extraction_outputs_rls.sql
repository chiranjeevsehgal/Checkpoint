-- +goose Up
-- The ingestion API now reads and mutates the extraction worker's output on
-- behalf of the signed-in user, so these tables need tenant isolation and a
-- request-role grant. The worker keeps its BYPASSRLS grants, so its
-- replace-per-audio writes are unaffected.
--
-- Note: todos.user_id is UUID, but reminders/insights.user_id is TEXT
-- (00005 predates the UUID hardening in 00002). The policies therefore cast
-- per table; normalizing those two tables later would need these updated.
ALTER TABLE todos ENABLE ROW LEVEL SECURITY;
ALTER TABLE reminders ENABLE ROW LEVEL SECURITY;
ALTER TABLE insights ENABLE ROW LEVEL SECURITY;

CREATE POLICY todos_tenant ON todos
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

CREATE POLICY reminders_tenant ON reminders
    USING (user_id = nullif(current_setting('app.user_id', true), ''))
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), ''));

CREATE POLICY insights_tenant ON insights
    USING (user_id = nullif(current_setting('app.user_id', true), ''))
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), ''));

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_request;
        GRANT SELECT, UPDATE, DELETE ON todos TO checkpoint_request;
        GRANT SELECT, DELETE ON reminders TO checkpoint_request;
        GRANT SELECT, DELETE ON insights TO checkpoint_request;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP POLICY IF EXISTS insights_tenant ON insights;
DROP POLICY IF EXISTS reminders_tenant ON reminders;
DROP POLICY IF EXISTS todos_tenant ON todos;
ALTER TABLE insights DISABLE ROW LEVEL SECURITY;
ALTER TABLE reminders DISABLE ROW LEVEL SECURITY;
ALTER TABLE todos DISABLE ROW LEVEL SECURITY;
