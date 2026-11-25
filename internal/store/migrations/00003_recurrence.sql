-- +goose Up
-- Repeating tasks (ADR-0013): a canonical RRULE subset. Completing one moves it
-- to its next date and logs the completion here, so history survives.
ALTER TABLE tasks ADD COLUMN recurrence TEXT;

CREATE TABLE task_completions (
    id            TEXT PRIMARY KEY,
    owner_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_id       TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    due_date      TEXT,
    completed_at  TEXT NOT NULL
);
CREATE INDEX task_completions_owner ON task_completions(owner_id, completed_at);

-- +goose Down
DROP TABLE task_completions;
ALTER TABLE tasks DROP COLUMN recurrence;
