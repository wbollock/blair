-- +goose Up
-- Daily plans (ADR-0014): the tasks picked to work on each day. Rows are kept
-- after the day passes; they are the history behind "keeps slipping".
CREATE TABLE plan_items (
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_date  TEXT NOT NULL,
    task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    added_at   TEXT NOT NULL,
    PRIMARY KEY (owner_id, plan_date, task_id)
);
CREATE INDEX plan_items_task ON plan_items(task_id, plan_date);

-- +goose Down
DROP TABLE plan_items;
