-- +goose Up
-- Single table for all users; user_id is the username namespace. Every
-- retrieval query MUST scope by user_id, e.g.:
--   SELECT chunk_text, 1 - (embedding <=> $query) AS similarity
--   FROM embeddings
--   WHERE user_id = $user
--   ORDER BY embedding <=> $query
--   LIMIT 10;
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE embeddings (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      TEXT NOT NULL,
    audio_id     TEXT NOT NULL,
    chunk_index  INTEGER NOT NULL,
    chunk_text   TEXT NOT NULL,
    language     TEXT,
    model        TEXT NOT NULL,
    embedding    vector(1024) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id, chunk_index)
);

CREATE INDEX embeddings_user_id_idx ON embeddings (user_id);
CREATE INDEX embeddings_audio_id_idx ON embeddings (audio_id);

-- HNSW over cosine distance; bge-m3 embeddings are L2-normalized on ingest.
CREATE INDEX embeddings_embedding_hnsw_idx ON embeddings USING hnsw (embedding vector_cosine_ops);

-- +goose Down
DROP TABLE embeddings;
