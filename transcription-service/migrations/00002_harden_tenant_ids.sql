-- +goose Up
ALTER TABLE transcripts
    ALTER COLUMN audio_id TYPE UUID USING audio_id::uuid,
    ALTER COLUMN user_id TYPE UUID USING user_id::uuid,
    ALTER COLUMN user_id SET NOT NULL;

-- +goose Down
ALTER TABLE transcripts
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN audio_id TYPE TEXT,
    ALTER COLUMN user_id TYPE TEXT;
