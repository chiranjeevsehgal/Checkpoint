-- +goose Up
-- Delivery ledger for reminder push notifications. Sending is driven by the
-- reminders table; this table only claims and de-duplicates deliveries so a
-- re-extraction (which replaces reminder rows with new ids) cannot double-send.
-- fire_at is part of the key so a rescheduled reminder becomes a new delivery.
CREATE TABLE notification_deliveries (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id       UUID NOT NULL,
    audio_id      UUID NOT NULL,
    reminder_text TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('advance', 'due')),
    fire_at       TIMESTAMPTZ NOT NULL,
    status        TEXT NOT NULL DEFAULT 'processing'
                  CHECK (status IN ('processing', 'sent', 'failed')),
    attempts      INT NOT NULL DEFAULT 0,
    last_error    TEXT,
    sent_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id, reminder_text, fire_at, kind)
);

-- Serves the retention sweep; claim/reclaim filters on (status, updated_at).
CREATE INDEX notification_deliveries_cleanup_idx
    ON notification_deliveries (updated_at);

-- The worker connects as the least-privilege checkpoint_worker role.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON notification_deliveries TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS notification_deliveries;
