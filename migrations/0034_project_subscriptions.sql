-- Project subscriptions: GitHub PR / CI webhooks → coordinator notify.
CREATE SEQUENCE IF NOT EXISTS project_subscription_key_seq;

CREATE TABLE IF NOT EXISTS project_subscriptions (
    id            TEXT PRIMARY KEY,
    key           TEXT NOT NULL UNIQUE,
    project_id    TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    kind          TEXT NOT NULL
        CHECK (kind IN ('github_pr', 'github_ci')),
    repo          TEXT NOT NULL DEFAULT '',
    secret        TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    last_event_at TIMESTAMPTZ,
    last_status   TEXT NOT NULL DEFAULT '',
    last_summary  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS project_subscriptions_project_idx
    ON project_subscriptions (project_id, kind);
CREATE UNIQUE INDEX IF NOT EXISTS project_subscriptions_project_kind_idx
    ON project_subscriptions (project_id, kind);
