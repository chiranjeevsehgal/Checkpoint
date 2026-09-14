-- +goose Up
ALTER TABLE transcripts
    ALTER COLUMN audio_id TYPE UUID USING audio_id::uuid,
    ALTER COLUMN user_id TYPE UUID USING user_id::uuid,
    ALTER COLUMN user_id SET NOT NULL;

-- The ingestion deletion worker purges transcripts by user_id.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT SELECT, DELETE ON transcripts TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE transcripts
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN audio_id TYPE TEXT,
    ALTER COLUMN user_id TYPE TEXT;
