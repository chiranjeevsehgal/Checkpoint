-- +goose Up
ALTER TABLE embeddings
    ALTER COLUMN user_id TYPE UUID USING user_id::uuid,
    ALTER COLUMN audio_id TYPE UUID USING audio_id::uuid;

-- +goose Down
ALTER TABLE embeddings
    ALTER COLUMN user_id TYPE TEXT,
    ALTER COLUMN audio_id TYPE TEXT;
