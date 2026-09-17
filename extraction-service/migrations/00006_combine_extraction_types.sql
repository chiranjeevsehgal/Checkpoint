-- +goose Up
-- One combined 'all' job per audio supersedes the old per-type queue rows.
-- Queue rows are transient (outputs live in todos/reminders/insights), so the
-- old rows are discarded rather than migrated.
DELETE FROM extraction_jobs WHERE extraction_type IN ('todo', 'reminder', 'insight');
ALTER TABLE extraction_jobs ALTER COLUMN extraction_type SET DEFAULT 'all';

-- +goose Down
-- Deleted queue rows cannot be reconstructed; restore the original default.
ALTER TABLE extraction_jobs ALTER COLUMN extraction_type SET DEFAULT 'todo';
SELECT 1;
