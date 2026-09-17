-- +goose Up
ALTER TABLE extraction_jobs
    ALTER COLUMN audio_id TYPE UUID USING audio_id::uuid,
    ALTER COLUMN user_id TYPE UUID USING user_id::uuid;

ALTER TABLE todos
    ALTER COLUMN audio_id TYPE UUID USING audio_id::uuid,
    ALTER COLUMN user_id TYPE UUID USING user_id::uuid;

-- The dedicated worker role is granted least-privilege access to the tables
-- it owns, mirroring transcription-service's 00002.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON extraction_jobs TO checkpoint_worker;
        GRANT SELECT, INSERT, DELETE ON todos TO checkpoint_worker;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE todos
    ALTER COLUMN audio_id TYPE TEXT,
    ALTER COLUMN user_id TYPE TEXT;
ALTER TABLE extraction_jobs
    ALTER COLUMN audio_id TYPE TEXT,
    ALTER COLUMN user_id TYPE TEXT;
