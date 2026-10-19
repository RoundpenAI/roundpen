-- Project workers (serial delegate queue) + issues.project_id.
-- Session kind "worker" stays out of the assistant primary-chat list.

DO $$
BEGIN
    ALTER TABLE agent_sessions DROP CONSTRAINT IF EXISTS agent_sessions_kind_ck;
    ALTER TABLE agent_sessions
        ADD CONSTRAINT agent_sessions_kind_ck
        CHECK (kind IN ('chat', 'routine', 'worker'));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

ALTER TABLE issues
    ADD COLUMN IF NOT EXISTS project_id TEXT REFERENCES projects (id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS issues_project_idx
    ON issues (project_id, updated_at DESC)
    WHERE project_id IS NOT NULL;

CREATE SEQUENCE IF NOT EXISTS project_worker_key_seq;

CREATE TABLE IF NOT EXISTS project_workers (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    project_id   TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    session_id   TEXT NOT NULL DEFAULT '',
    issue_id     TEXT REFERENCES issues (id) ON DELETE SET NULL,
    title        TEXT NOT NULL,
    brief        TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    error        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS project_workers_project_idx
    ON project_workers (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS project_workers_user_queue_idx
    ON project_workers (user_id, status, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS project_workers_one_running_per_user_idx
    ON project_workers (user_id)
    WHERE status = 'running';
