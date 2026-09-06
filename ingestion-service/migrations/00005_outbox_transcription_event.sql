-- +goose Up
-- Rename the VAD outbox event to the transcription queue event.
-- Existing PENDING/PROCESSING rows are updated so the Kafka dispatcher
-- drains them; DELIVERED/FAILED history keeps its outcome, only the type
-- label changes. The unique (aggregate_id, event_type) index is renamed
-- to match the new terminology (no data change).
UPDATE outbox_events
SET event_type = 'TRANSCRIPTION_REQUESTED'
WHERE event_type = 'AUDIO_READY_FOR_VAD';

ALTER INDEX IF EXISTS idx_outbox_vad_upload RENAME TO idx_outbox_tx_upload;

-- +goose Down
ALTER INDEX IF EXISTS idx_outbox_tx_upload RENAME TO idx_outbox_vad_upload;

UPDATE outbox_events
SET event_type = 'AUDIO_READY_FOR_VAD'
WHERE event_type = 'TRANSCRIPTION_REQUESTED';
