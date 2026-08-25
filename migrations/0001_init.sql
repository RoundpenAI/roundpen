-- 0001_init.sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS sandboxes (
    id              TEXT PRIMARY KEY,
    container_id    TEXT NOT NULL DEFAULT '',
    image           TEXT NOT NULL,
    status          TEXT NOT NULL,
    workspace_id    TEXT NOT NULL DEFAULT '',
    workspace_path  TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}',
    ttl_seconds     INTEGER NOT NULL DEFAULT 1800,
    expires_at      TIMESTAMPTZ,
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS sandboxes_status_idx ON sandboxes (status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS sandboxes_expires_at_idx ON sandboxes (expires_at) WHERE deleted_at IS NULL;
