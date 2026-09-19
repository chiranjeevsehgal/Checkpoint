-- +goose Up
-- Per-account MCP access keys. The full secret is shown once at creation and
-- only its SHA-256 is stored; key_prefix is a truncated display value.
CREATE TABLE mcp_access_keys (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      UUID NOT NULL,
    name         TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,
    key_hash     BYTEA NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX mcp_access_keys_hash_idx
    ON mcp_access_keys (key_hash) WHERE revoked_at IS NULL;

ALTER TABLE mcp_access_keys ENABLE ROW LEVEL SECURITY;

CREATE POLICY mcp_access_keys_tenant ON mcp_access_keys
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_request;
        GRANT SELECT, INSERT, UPDATE, DELETE ON mcp_access_keys TO checkpoint_request;
    END IF;

    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, DELETE ON mcp_access_keys TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- The MCP resolves a presented key by its hash before it knows the user, so
-- this SECURITY DEFINER function is the one cross-tenant read. It updates
-- last_used_at and returns only the owning user id, never key material.
-- search_path is pinned against search_path attacks. EXECUTE is granted to
-- checkpoint_mcp by the mcp-service migration once that role exists.
-- +goose StatementBegin
CREATE FUNCTION mcp_resolve_key(p_key_hash bytea) RETURNS uuid
    LANGUAGE sql
    SECURITY DEFINER
    SET search_path = public, pg_temp
AS $$
    UPDATE mcp_access_keys
       SET last_used_at = NOW()
     WHERE key_hash = p_key_hash AND revoked_at IS NULL
    RETURNING user_id;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS mcp_resolve_key(bytea);
DROP POLICY IF EXISTS mcp_access_keys_tenant ON mcp_access_keys;
DROP TABLE IF EXISTS mcp_access_keys;
