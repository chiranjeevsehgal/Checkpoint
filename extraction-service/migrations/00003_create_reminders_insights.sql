-- +goose Up
-- Same replace-never-accumulate contract as todos: rows are deleted and
-- re-inserted per (user, audio) in one transaction on every successful
-- extraction run, so redeliveries and re-runs can neither duplicate nor
-- leave stale rows.

-- Time-bound commitments; remind_at is the LLM-resolved due time, NULL when
-- the statement was time-bound but no concrete time could be determined.
-- The partial index serves the future scheduler's "due reminders" scan.
CREATE TABLE reminders (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     TEXT NOT NULL,
    audio_id    TEXT NOT NULL,
    text        TEXT NOT NULL,
    remind_at   TIMESTAMPTZ,
    model       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id, text)
);

CREATE INDEX reminders_user_id_idx ON reminders (user_id);
CREATE INDEX reminders_audio_id_idx ON reminders (audio_id);
CREATE INDEX reminders_due_idx ON reminders (remind_at) WHERE remind_at IS NOT NULL;

-- Reflections, realizations, ideas, decisions — not action items.
CREATE TABLE insights (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     TEXT NOT NULL,
    audio_id    TEXT NOT NULL,
    text        TEXT NOT NULL,
    model       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, audio_id, text)
);

CREATE INDEX insights_user_id_idx ON insights (user_id);
CREATE INDEX insights_audio_id_idx ON insights (audio_id);

-- +goose Down
DROP TABLE insights;
DROP TABLE reminders;
