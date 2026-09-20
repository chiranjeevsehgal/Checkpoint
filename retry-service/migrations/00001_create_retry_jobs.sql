-- +goose Up
-- retry_jobs is the durable retry queue. A failing service hands its failed
-- message to retry.jobs.v1; the consumer upserts one row keyed by
-- (source_topic, original_event_id) and the dispatcher re-publishes the
-- original payload back to the source topic once next_attempt_at arrives.
-- The wait is data, not memory, so a restart neither loses nor rushes a
-- pending retry.
CREATE TABLE retry_jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    original_event_id TEXT NOT NULL,
    source_service    TEXT NOT NULL,
    source_topic      TEXT NOT NULL,
    message_key       TEXT,
    user_id           UUID,
    original_payload  JSONB NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending','processing','dispatched','failed','skipped')),
    attempts          INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_stage        TEXT,
    last_error_code   TEXT,
    last_error        TEXT,
    attempt_log       JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    dispatched_at     TIMESTAMPTZ,
    failed_at         TIMESTAMPTZ,
    UNIQUE (source_topic, original_event_id)
);

-- Drives the dispatcher's due-claim.
CREATE INDEX retry_jobs_due_idx
    ON retry_jobs (next_attempt_at)
    WHERE status = 'pending';

-- Drives the account-deletion purge.
CREATE INDEX retry_jobs_user_idx ON retry_jobs (user_id);

-- Drives the retention prune of terminal rows.
CREATE INDEX retry_jobs_retention_idx
    ON retry_jobs (updated_at)
    WHERE status IN ('dispatched','failed','skipped');

-- +goose Down
DROP TABLE retry_jobs;
