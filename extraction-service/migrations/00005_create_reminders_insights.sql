-- +goose Up
-- Same replace-never-accumulate contract as todos: rows are deleted and
-- re-inserted per (user, audio) in one transaction on every successful
-- extraction run, so redeliveries and re-runs can neither duplicate nor
-- leave stale rows.

-- Time-bound commitments; remind_at is the LLM-resolved due time, NULL when
-- the statement was time-bound but no concrete time could be determined.
-- The partial index serves the future scheduler's "due reminders" scan.
CREATE TABLE IF NOT EXISTS reminders (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     TEXT NOT NULL,
    audio_id    TEXT NOT NULL,
    text        TEXT NOT NULL,
    remind_at   TIMESTAMPTZ,
    model       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id, text)
);

CREATE INDEX IF NOT EXISTS reminders_user_id_idx ON reminders (user_id);
CREATE INDEX IF NOT EXISTS reminders_audio_id_idx ON reminders (audio_id);
CREATE INDEX IF NOT EXISTS reminders_due_idx ON reminders (remind_at) WHERE remind_at IS NOT NULL;

-- Reflections, realizations, ideas, decisions — not action items.
CREATE TABLE IF NOT EXISTS insights (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     TEXT NOT NULL,
    audio_id    TEXT NOT NULL,
    text        TEXT NOT NULL,
    model       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id, text)
);

CREATE INDEX IF NOT EXISTS insights_user_id_idx ON insights (user_id);
CREATE INDEX IF NOT EXISTS insights_audio_id_idx ON insights (audio_id);

-- The extraction worker connects as the least-privilege checkpoint_worker
-- role; mirror the grants in 00002_harden_tenant_ids.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, DELETE ON reminders TO checkpoint_worker;
        GRANT SELECT, INSERT, DELETE ON insights TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS insights;
DROP TABLE IF EXISTS reminders;
