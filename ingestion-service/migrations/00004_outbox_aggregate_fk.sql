-- +goose Up
-- The outbox is owned by ingestion: an AUDIO_READY_FOR_VAD event must
-- never refer to a nonexistent upload.
ALTER TABLE outbox_events
    ADD CONSTRAINT fk_outbox_upload FOREIGN KEY (aggregate_id) REFERENCES uploads(id);

-- +goose Down
ALTER TABLE outbox_events DROP CONSTRAINT IF EXISTS fk_outbox_upload;
