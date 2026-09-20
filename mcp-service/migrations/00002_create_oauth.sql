-- +goose Up
-- OAuth 2.1 state for the MCP authorization server (mcp-service). The AS and
-- resource server are the same process: the SDK routes call into the provider,
-- which persists here. Tokens and codes are stored as SHA-256 hashes only.
CREATE TABLE oauth_clients (
    client_id     TEXT PRIMARY KEY,
    metadata      JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One pending authorization request per /authorize call, consumed at consent.
CREATE TABLE oauth_auth_requests (
    id                              UUID PRIMARY KEY,
    client_id                       TEXT NOT NULL,
    redirect_uri                    TEXT NOT NULL,
    redirect_uri_provided_explicitly BOOLEAN NOT NULL DEFAULT TRUE,
    scopes                          TEXT NOT NULL,
    code_challenge                  TEXT NOT NULL,
    state                           TEXT,
    resource                        TEXT,
    expires_at                      TIMESTAMPTZ NOT NULL,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE oauth_authorization_codes (
    code_hash     BYTEA PRIMARY KEY,
    client_id     TEXT NOT NULL,
    user_id       UUID NOT NULL,
    redirect_uri  TEXT NOT NULL,
    scopes        TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    resource      TEXT,
    expires_at    TIMESTAMPTZ NOT NULL,
    consumed_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- family_id links the access/refresh pair issued together so revoking either
-- revokes the whole pair. Rotation issues a new family and revokes the old.
CREATE TABLE oauth_tokens (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash  BYTEA NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
    family_id   UUID NOT NULL,
    user_id     UUID NOT NULL,
    client_id   TEXT NOT NULL,
    scopes      TEXT NOT NULL,
    resource    TEXT,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX oauth_tokens_hash_idx ON oauth_tokens (token_hash) WHERE revoked_at IS NULL;
CREATE INDEX oauth_tokens_user_idx ON oauth_tokens (user_id);
CREATE INDEX oauth_tokens_family_idx ON oauth_tokens (family_id);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON
            oauth_clients, oauth_auth_requests, oauth_authorization_codes, oauth_tokens
            TO checkpoint_worker;
        GRANT USAGE, SELECT ON SEQUENCE oauth_tokens_id_seq TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS oauth_tokens;
DROP TABLE IF EXISTS oauth_authorization_codes;
DROP TABLE IF EXISTS oauth_auth_requests;
DROP TABLE IF EXISTS oauth_clients;
