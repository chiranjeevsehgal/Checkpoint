-- +goose Up
-- One narrative summary per (user, period, period_start). period_start is the
-- user's local date: the day itself for 'daily', the Monday for 'weekly'.
-- Rows are upserted on re-runs, so late-arriving audio can refresh a period.
CREATE TABLE IF NOT EXISTS summaries (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      TEXT NOT NULL,
    period       TEXT NOT NULL,
    period_start DATE NOT NULL,
    text         TEXT NOT NULL,
    model        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT summaries_period_check CHECK (period IN ('daily', 'weekly')),
    UNIQUE (user_id, period, period_start)
);

CREATE INDEX IF NOT EXISTS summaries_user_period_idx
    ON summaries (user_id, period, period_start DESC);

-- The rollup worker connects as the least-privilege checkpoint_worker role.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT USAGE ON SCHEMA public TO checkpoint_worker;
        GRANT SELECT, INSERT, UPDATE, DELETE ON summaries TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS summaries;
