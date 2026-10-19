-- Per-user directory in the fnOS user's own files (not @appshare).
CREATE TABLE IF NOT EXISTS user_workspace_roots (
    user_id    TEXT PRIMARY KEY REFERENCES users (username) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
