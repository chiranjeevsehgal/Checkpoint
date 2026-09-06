-- +goose Up
-- Rename the VAD outbox event to the transcription queue event.
-- Existing PENDING/PROCESSING rows are updated so the Kafka dispatcher
-- drains them; DELIVERED/FAILED history keeps its outcome, only the type
-- label changes. The unique (aggregate_id, event_type) index is renamed
-- to match the new terminology (no data change).
-- Payload JSONB is backfilled in the same transaction: event_type,
-- schema_version (1->2) and data.user_id (from uploads). Checksum stays
-- absent when the client did not confirm one (omitempty).
UPDATE outbox_events e
SET event_type = 'TRANSCRIPTION_REQUESTED',
    payload = jsonb_set(
        jsonb_set(
            jsonb_set(e.payload, '{event_type}', '"TRANSCRIPTION_REQUESTED"'),
            '{schema_version}', '2'
        ),
        '{data,user_id}', to_jsonb(u.user_id)
    )
FROM uploads u
WHERE e.event_type = 'AUDIO_READY_FOR_VAD'
  AND e.aggregate_id = u.id;

-- Orphan rows (no uploads match; FK normally prevents this) still get the
-- type and version rewrite without user_id so no old payload survives.
UPDATE outbox_events
SET event_type = 'TRANSCRIPTION_REQUESTED',
    payload = jsonb_set(
        jsonb_set(payload, '{event_type}', '"TRANSCRIPTION_REQUESTED"'),
        '{schema_version}', '2'
    )
WHERE event_type = 'AUDIO_READY_FOR_VAD';

ALTER INDEX IF EXISTS idx_outbox_vad_upload RENAME TO idx_outbox_tx_upload;

-- +goose Down
ALTER INDEX IF EXISTS idx_outbox_tx_upload RENAME TO idx_outbox_vad_upload;

-- Down reverts labels only; data.user_id/checksum are kept (old code
-- ignores unknown JSON fields, and Up can restore without data loss).
UPDATE outbox_events
SET event_type = 'AUDIO_READY_FOR_VAD',
    payload = jsonb_set(
        jsonb_set(payload, '{event_type}', '"AUDIO_READY_FOR_VAD"'),
        '{schema_version}', '1'
    )
WHERE event_type = 'TRANSCRIPTION_REQUESTED';
