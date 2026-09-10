-- Agent Web UI sessions (ACP gateway)
CREATE TABLE IF NOT EXISTS agent_sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    title        TEXT NOT NULL DEFAULT '',
    provider_id  TEXT NOT NULL DEFAULT 'mock',
    sandbox_id   TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS agent_sessions_user_idx ON agent_sessions (user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS agent_messages (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES agent_sessions (id) ON DELETE CASCADE,
    role         TEXT NOT NULL,
    content      TEXT NOT NULL DEFAULT '',
    meta         JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS agent_messages_session_idx ON agent_messages (session_id, created_at);
