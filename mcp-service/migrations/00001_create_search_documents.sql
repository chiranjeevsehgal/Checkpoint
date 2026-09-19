-- +goose Up
-- Derived read model for MCP retrieval. It is rebuildable from the source
-- tables (transcripts, embeddings, todos, reminders, insights, summaries)
-- and is the only content table the MCP role may read. RLS scopes every
-- row to app.user_id, so a missing predicate cannot leak across accounts.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE search_documents (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id       UUID NOT NULL,
    source_type   TEXT NOT NULL CHECK (source_type IN ('transcript','todo','reminder','insight','summary')),
    source_id     TEXT NOT NULL,
    audio_id      UUID,
    chunk_index   INTEGER NOT NULL DEFAULT -1,
    content       TEXT NOT NULL,
    language      TEXT,
    occurred_at   TIMESTAMPTZ NOT NULL,
    recorded_at   TIMESTAMPTZ,
    reminded_at   TIMESTAMPTZ,
    is_done       BOOLEAN,
    important     BOOLEAN,
    period        TEXT,
    period_start  DATE,
    model         TEXT,
    content_hash  TEXT NOT NULL,
    embedding     vector(1024),
    indexed_at    TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, source_type, source_id, chunk_index)
);

CREATE INDEX search_documents_user_time_idx ON search_documents (user_id, occurred_at DESC);
CREATE INDEX search_documents_pending_idx
    ON search_documents (updated_at) WHERE embedding IS NULL AND indexed_at IS NULL;
CREATE INDEX search_documents_hnsw_idx
    ON search_documents USING hnsw (embedding vector_cosine_ops) WHERE embedding IS NOT NULL;

ALTER TABLE search_documents ENABLE ROW LEVEL SECURITY;

CREATE POLICY search_documents_tenant ON search_documents
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

-- Indexer watermark, one row per source family.
CREATE TABLE mcp_index_state (
    name       TEXT PRIMARY KEY,
    watermark  TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The MCP role is created by migrate.py before this runs; the grants stay
-- conditional so `migrate.py status` on a role-less database still works.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_mcp') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_mcp;
        GRANT SELECT ON search_documents, user_settings, mcp_access_keys TO checkpoint_mcp;
        GRANT EXECUTE ON FUNCTION mcp_resolve_key(bytea) TO checkpoint_mcp;
    END IF;

    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON search_documents, mcp_index_state TO checkpoint_worker;
        GRANT USAGE, SELECT ON SEQUENCE search_documents_id_seq TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS mcp_index_state;
DROP POLICY IF EXISTS search_documents_tenant ON search_documents;
DROP TABLE IF EXISTS search_documents;
