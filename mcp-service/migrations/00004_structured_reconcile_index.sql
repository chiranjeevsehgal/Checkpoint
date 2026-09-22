-- +goose Up
-- The reconcile pass scans structured documents (todo/reminder/insight) to
-- find rows whose source changed or vanished. This partial index keeps those
-- scans off a sequential scan of the whole read model.
CREATE INDEX IF NOT EXISTS search_documents_structured_idx
    ON search_documents (source_type, user_id, source_id)
    WHERE source_type IN ('todo', 'reminder', 'insight') AND chunk_index = -1;

-- +goose Down
DROP INDEX IF EXISTS search_documents_structured_idx;
