-- +goose Up
-- The items API now edits reminder and insight text, so checkpoint_request
-- needs UPDATE on those tables. 00016 granted it on todos but only
-- SELECT/DELETE on reminders and insights, which predated text editing.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        GRANT UPDATE ON reminders TO checkpoint_request;
        GRANT UPDATE ON insights TO checkpoint_request;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        REVOKE UPDATE ON reminders FROM checkpoint_request;
        REVOKE UPDATE ON insights FROM checkpoint_request;
    END IF;
END
$$;
-- +goose StatementEnd
