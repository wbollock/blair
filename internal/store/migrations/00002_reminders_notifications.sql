-- +goose Up
-- One reminder per task (ADR-0012). reminded_at is set when it fires and cleared
-- whenever remind_at changes, so a new time fires again.
ALTER TABLE tasks ADD COLUMN remind_at TEXT;
ALTER TABLE tasks ADD COLUMN reminded_at TEXT;
CREATE INDEX tasks_remind_due ON tasks(remind_at) WHERE remind_at IS NOT NULL AND reminded_at IS NULL;

CREATE TABLE notifications (
    id          TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    task_id     TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    text        TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    read_at     TEXT
);
CREATE INDEX notifications_owner ON notifications(owner_id, created_at);

-- +goose Down
DROP TABLE notifications;
DROP INDEX tasks_remind_due;
ALTER TABLE tasks DROP COLUMN reminded_at;
ALTER TABLE tasks DROP COLUMN remind_at;
