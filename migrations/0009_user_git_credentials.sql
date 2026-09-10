CREATE TABLE IF NOT EXISTS user_git_credentials (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    provider     TEXT NOT NULL DEFAULT 'gitea',
    host         TEXT NOT NULL,
    username     TEXT NOT NULL DEFAULT '',
    label        TEXT NOT NULL DEFAULT '',
    token        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, host)
);
CREATE INDEX IF NOT EXISTS user_git_credentials_user_idx ON user_git_credentials (user_id, host);
