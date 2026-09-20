-- +goose Up
-- The dedicated worker role gets least-privilege access to the table this
-- service owns, mirroring the other workers' 00002 grants.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON retry_jobs TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        REVOKE SELECT, INSERT, UPDATE, DELETE ON retry_jobs FROM checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd
