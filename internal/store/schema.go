package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS projects (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    cwd     TEXT UNIQUE NOT NULL,
    name    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    project_id  INTEGER NOT NULL REFERENCES projects(id),
    first_seen  TEXT NOT NULL,
    last_seen   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    started_at      TEXT NOT NULL,
    ended_at        TEXT,
    max_age_seconds INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    uuid                  TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL REFERENCES sessions(id),
    ts                    TEXT NOT NULL,
    type                  TEXT NOT NULL,
    model                 TEXT,
    in_tokens             INTEGER NOT NULL DEFAULT 0,
    out_tokens            INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens     INTEGER NOT NULL DEFAULT 0,
    cache_create_tokens   INTEGER NOT NULL DEFAULT 0,
    cost_eur              REAL,
    task_id               TEXT REFERENCES tasks(id)
);

CREATE INDEX IF NOT EXISTS idx_events_ts        ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_session   ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_task      ON events(task_id);

CREATE TABLE IF NOT EXISTS file_offsets (
    path   TEXT PRIMARY KEY,
    offset INTEGER NOT NULL,
    size   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`
