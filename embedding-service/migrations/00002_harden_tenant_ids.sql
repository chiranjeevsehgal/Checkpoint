-- +goose Up
ALTER TABLE embeddings
    ALTER COLUMN user_id TYPE UUID USING user_id::uuid,
    ALTER COLUMN audio_id TYPE UUID USING audio_id::uuid;

-- The ingestion deletion worker purges embeddings by user_id.
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        GRANT SELECT, DELETE ON embeddings TO checkpoint_worker;
    END IF;
END
$$;

-- +goose Down
ALTER TABLE embeddings
    ALTER COLUMN user_id TYPE TEXT,
    ALTER COLUMN audio_id TYPE TEXT;
