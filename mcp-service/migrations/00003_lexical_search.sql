-- +goose Up
-- Full-text index backing the lexical channel of hybrid retrieval
-- (ReadStore.lexical_search). Expression index on the simple dictionary so
-- stemming never hides exact terms like names or invoice numbers.
CREATE INDEX IF NOT EXISTS search_documents_content_fts_idx
    ON search_documents USING gin (to_tsvector('simple', content));

-- +goose Down
DROP INDEX IF EXISTS search_documents_content_fts_idx;
