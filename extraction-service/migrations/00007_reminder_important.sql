-- +goose Up
-- Marks a reminder as important enough to receive an advance push. The due
-- push is unconditional; only the advance is gated on this flag. Existing
-- rows default to true so they keep their current behaviour.
ALTER TABLE reminders ADD COLUMN important BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down
ALTER TABLE reminders DROP COLUMN IF EXISTS important;
