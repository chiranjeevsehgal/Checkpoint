-- +goose Up
-- Completion state for todos. Owned by the (future) todo-management API;
-- the worker never writes it: fresh todos always land not-done via the
-- column default. Note replace-semantics: re-extracting an audio wipes
-- its todos back to not-done.
ALTER TABLE todos ADD COLUMN IF NOT EXISTS is_done BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE todos DROP COLUMN IF EXISTS is_done;
