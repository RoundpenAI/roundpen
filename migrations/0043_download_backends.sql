-- User-configured download clients (qBittorrent, …) for NAS media assistants.
CREATE TABLE IF NOT EXISTS download_backends (
    id                 TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    type               TEXT NOT NULL DEFAULT 'qbittorrent'
        CHECK (type IN ('qbittorrent', 'aria2', 'webhook')),
    base_url           TEXT NOT NULL,
    username           TEXT NOT NULL DEFAULT '',
    password           TEXT NOT NULL DEFAULT '',
    default_save_path  TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS download_backends_user_idx
    ON download_backends (user_id, updated_at DESC);
