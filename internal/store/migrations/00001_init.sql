-- +goose Up
-- Timestamps are TEXT in UTC, format 2006-01-02T15:04:05.000Z, so they sort lexically.
-- Every user-owned table carries owner_id; every query filters on it (ADR-0003).

CREATE TABLE users (
    id                    TEXT PRIMARY KEY,
    email                 TEXT NOT NULL UNIQUE COLLATE NOCASE,
    display_name          TEXT NOT NULL DEFAULT '',
    password_hash         TEXT NOT NULL,
    timezone              TEXT NOT NULL DEFAULT 'UTC',
    week_start            TEXT NOT NULL DEFAULT 'monday',
    is_admin              INTEGER NOT NULL DEFAULT 0,
    disabled_at           TEXT,
    must_change_password  INTEGER NOT NULL DEFAULT 0,
    totp_secret_enc       BLOB,
    totp_enabled_at       TEXT,
    totp_last_step        INTEGER NOT NULL DEFAULT 0,
    settings              TEXT NOT NULL DEFAULT '{}',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);

CREATE TABLE sessions (
    id                   TEXT PRIMARY KEY,
    user_id              TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash           BLOB NOT NULL UNIQUE,
    mfa_pending          INTEGER NOT NULL DEFAULT 0,
    mfa_attempts         INTEGER NOT NULL DEFAULT 0,
    ip                   TEXT NOT NULL DEFAULT '',
    user_agent           TEXT NOT NULL DEFAULT '',
    created_at           TEXT NOT NULL,
    last_seen_at         TEXT NOT NULL,
    expires_at           TEXT NOT NULL,
    absolute_expires_at  TEXT NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE api_tokens (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    token_hash    BLOB NOT NULL UNIQUE,
    prefix        TEXT NOT NULL,
    scopes        TEXT NOT NULL,
    expires_at    TEXT,
    last_used_at  TEXT,
    created_at    TEXT NOT NULL
);
CREATE INDEX api_tokens_user ON api_tokens(user_id);

CREATE TABLE recovery_codes (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    used_at    TEXT
);
CREATE INDEX recovery_codes_user ON recovery_codes(user_id);

CREATE TABLE invites (
    id          TEXT PRIMARY KEY,
    token_hash  BLOB NOT NULL UNIQUE,
    email       TEXT,
    created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TEXT NOT NULL,
    used_at     TEXT,
    used_by     TEXT,
    created_at  TEXT NOT NULL
);

CREATE TABLE audit_events (
    id          TEXT PRIMARY KEY,
    user_id     TEXT REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    ip          TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    detail      TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);
CREATE INDEX audit_events_user ON audit_events(user_id, created_at);

CREATE TABLE projects (
    id               TEXT PRIMARY KEY,
    owner_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    color            TEXT NOT NULL DEFAULT '',
    icon             TEXT NOT NULL DEFAULT '',
    default_view     TEXT NOT NULL DEFAULT 'list',
    position         TEXT NOT NULL,
    archived_at      TEXT,
    external_source  TEXT,
    external_id      TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX projects_owner ON projects(owner_id, position);
CREATE UNIQUE INDEX projects_external ON projects(owner_id, external_source, external_id) WHERE external_source IS NOT NULL;

CREATE TABLE columns (
    id          TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('todo','doing','waiting','done','cancelled')),
    position    TEXT NOT NULL,
    wip_limit   INTEGER,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX columns_project ON columns(project_id, position);

CREATE TABLE tasks (
    id               TEXT PRIMARY KEY,
    owner_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title            TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'todo' CHECK (status IN ('todo','doing','waiting','done','cancelled')),
    priority         INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
    project_id       TEXT REFERENCES projects(id) ON DELETE CASCADE,
    column_id        TEXT REFERENCES columns(id) ON DELETE SET NULL,
    due_date         TEXT,
    due_time         TEXT,
    position         TEXT NOT NULL,
    completed_at     TEXT,
    deleted_at       TEXT,
    external_source  TEXT,
    external_id      TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX tasks_owner_project ON tasks(owner_id, project_id, column_id, position);
CREATE INDEX tasks_owner_due ON tasks(owner_id, due_date);
CREATE INDEX tasks_owner_created ON tasks(owner_id, created_at);
CREATE INDEX tasks_owner_completed ON tasks(owner_id, completed_at);
CREATE UNIQUE INDEX tasks_external ON tasks(owner_id, external_source, external_id) WHERE external_source IS NOT NULL;

CREATE TABLE labels (
    id               TEXT PRIMARY KEY,
    owner_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name             TEXT NOT NULL COLLATE NOCASE,
    color            TEXT NOT NULL DEFAULT '',
    external_source  TEXT,
    external_id      TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1,
    UNIQUE (owner_id, name)
);
CREATE UNIQUE INDEX labels_external ON labels(owner_id, external_source, external_id) WHERE external_source IS NOT NULL;

CREATE TABLE task_labels (
    task_id   TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    label_id  TEXT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, label_id)
);
CREATE INDEX task_labels_label ON task_labels(label_id);

CREATE TABLE checklist_items (
    id          TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    text        TEXT NOT NULL,
    done        INTEGER NOT NULL DEFAULT 0,
    position    TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE INDEX checklist_task ON checklist_items(task_id, position);

CREATE TABLE notes (
    id               TEXT PRIMARY KEY,
    owner_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title            TEXT NOT NULL DEFAULT '',
    body             TEXT NOT NULL DEFAULT '',
    pinned           INTEGER NOT NULL DEFAULT 0,
    project_id       TEXT REFERENCES projects(id) ON DELETE SET NULL,
    deleted_at       TEXT,
    external_source  TEXT,
    external_id      TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX notes_owner ON notes(owner_id, updated_at);
CREATE UNIQUE INDEX notes_external ON notes(owner_id, external_source, external_id) WHERE external_source IS NOT NULL;

-- Full-text search over tasks and notes, kept in sync by triggers.
CREATE VIRTUAL TABLE search_fts USING fts5(
    kind UNINDEXED, ref_id UNINDEXED, owner_id UNINDEXED, title, body,
    tokenize = 'unicode61 remove_diacritics 2'
);

-- +goose StatementBegin
CREATE TRIGGER tasks_fts_ai AFTER INSERT ON tasks BEGIN
    INSERT INTO search_fts(kind, ref_id, owner_id, title, body) VALUES ('task', new.id, new.owner_id, new.title, new.description);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER tasks_fts_au AFTER UPDATE OF title, description ON tasks BEGIN
    UPDATE search_fts SET title = new.title, body = new.description WHERE kind = 'task' AND ref_id = new.id;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER tasks_fts_ad AFTER DELETE ON tasks BEGIN
    DELETE FROM search_fts WHERE kind = 'task' AND ref_id = old.id;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER notes_fts_ai AFTER INSERT ON notes BEGIN
    INSERT INTO search_fts(kind, ref_id, owner_id, title, body) VALUES ('note', new.id, new.owner_id, new.title, new.body);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER notes_fts_au AFTER UPDATE OF title, body ON notes BEGIN
    UPDATE search_fts SET title = new.title, body = new.body WHERE kind = 'note' AND ref_id = new.id;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER notes_fts_ad AFTER DELETE ON notes BEGIN
    DELETE FROM search_fts WHERE kind = 'note' AND ref_id = old.id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE search_fts;
DROP TABLE notes;
DROP TABLE checklist_items;
DROP TABLE task_labels;
DROP TABLE labels;
DROP TABLE tasks;
DROP TABLE columns;
DROP TABLE projects;
DROP TABLE audit_events;
DROP TABLE invites;
DROP TABLE recovery_codes;
DROP TABLE api_tokens;
DROP TABLE sessions;
DROP TABLE users;
