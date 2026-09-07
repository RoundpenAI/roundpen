CREATE TABLE IF NOT EXISTS browser_tasks (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    url          TEXT NOT NULL,
    brief        TEXT NOT NULL DEFAULT '',
    session_id   TEXT REFERENCES agent_sessions (id) ON DELETE SET NULL,
    status       TEXT NOT NULL DEFAULT 'open',
    prompt       TEXT NOT NULL DEFAULT '',
    report       JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS browser_tasks_user_idx ON browser_tasks (user_id, created_at DESC);
