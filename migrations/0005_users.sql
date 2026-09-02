-- Users + cookie sessions (password login and per-user API keys)
CREATE TABLE IF NOT EXISTS users (
    username        TEXT PRIMARY KEY,
    email           TEXT NOT NULL DEFAULT '',
    fullname        TEXT NOT NULL DEFAULT '',
    org_name        TEXT NOT NULL DEFAULT '',
    api_key         TEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT 'user',
    password_hash   TEXT,
    auth_provider   TEXT NOT NULL DEFAULT 'local',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_api_key ON users (api_key);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email
    ON users (lower(email))
    WHERE email <> '';

CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_agent   TEXT,
    ip           TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions (expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_sessions_token_hash ON sessions (token_hash);
