-- +goose Up
ALTER TABLE uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE devices ENABLE ROW LEVEL SECURITY;

CREATE POLICY uploads_tenant ON uploads
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

CREATE POLICY idempotency_keys_tenant ON idempotency_keys
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

CREATE POLICY devices_tenant ON devices
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- Claim a provisioned pendant for the transaction's app.user_id. All failure
-- causes raise the same message so callers cannot enumerate device state.
-- +goose StatementBegin
CREATE FUNCTION claim_device(p_device_id CHAR(32), p_claim_hash BYTEA)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    v_user_id UUID := nullif(current_setting('app.user_id', true), '')::uuid;
    v_state TEXT;
    v_owner UUID;
    v_hash BYTEA;
BEGIN
    IF v_user_id IS NULL THEN
        RAISE EXCEPTION 'device_claim_failed';
    END IF;

    SELECT d.state, d.user_id INTO v_state, v_owner
    FROM devices d
    WHERE d.device_id = p_device_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'device_claim_failed';
    END IF;

    IF v_state = 'owned' AND v_owner = v_user_id THEN
        RETURN;
    END IF;

    IF v_state <> 'unowned' THEN
        RAISE EXCEPTION 'device_claim_failed';
    END IF;

    SELECT c.claim_hash INTO v_hash
    FROM device_claim_credentials c
    WHERE c.device_id = p_device_id;

    IF v_hash IS NULL OR v_hash <> p_claim_hash THEN
        RAISE EXCEPTION 'device_claim_failed';
    END IF;

    IF EXISTS (SELECT 1 FROM devices WHERE user_id = v_user_id) THEN
        RAISE EXCEPTION 'device_claim_failed';
    END IF;

    UPDATE devices
    SET user_id = v_user_id, state = 'owned', claimed_at = NOW(), updated_at = NOW()
    WHERE device_id = p_device_id;
END;
$$;
-- +goose StatementEnd

-- Release only the caller's own pendant.
-- +goose StatementBegin
CREATE FUNCTION release_device()
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    v_user_id UUID := nullif(current_setting('app.user_id', true), '')::uuid;
BEGIN
    IF v_user_id IS NULL THEN
        RETURN;
    END IF;

    UPDATE devices
    SET user_id = NULL, state = 'unowned', claimed_at = NULL, updated_at = NOW()
    WHERE user_id = v_user_id;
END;
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION claim_device(CHAR(32), BYTEA) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION release_device() FROM PUBLIC;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_request;
        GRANT SELECT, INSERT, UPDATE ON uploads TO checkpoint_request;
        GRANT SELECT, INSERT, UPDATE, DELETE ON idempotency_keys TO checkpoint_request;
        GRANT SELECT, INSERT ON outbox_events TO checkpoint_request;
        GRANT SELECT ON devices TO checkpoint_request;
        GRANT EXECUTE ON FUNCTION claim_device(CHAR(32), BYTEA) TO checkpoint_request;
        GRANT EXECUTE ON FUNCTION release_device() TO checkpoint_request;
    END IF;

    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON uploads TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON idempotency_keys TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON devices TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON device_claim_credentials TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON outbox_events TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS claim_device(CHAR(32), BYTEA);
DROP FUNCTION IF EXISTS release_device();
DROP POLICY IF EXISTS devices_tenant ON devices;
DROP POLICY IF EXISTS idempotency_keys_tenant ON idempotency_keys;
DROP POLICY IF EXISTS uploads_tenant ON uploads;
ALTER TABLE devices DISABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys DISABLE ROW LEVEL SECURITY;
ALTER TABLE uploads DISABLE ROW LEVEL SECURITY;
