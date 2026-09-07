CREATE TABLE IF NOT EXISTS user_runtime (
    user_id       TEXT PRIMARY KEY REFERENCES users (username) ON DELETE CASCADE,
    agent_engine  TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
