-- Standing routines: a recurring task assigned to one assistant, plus each firing.
-- Short keys (RTN-n / RUN-n) come from per-install sequences. uuid stays the primary key.
-- agent_sessions.kind keeps a run's execution chat out of the assistant's primary session.

ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'chat';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'agent_sessions_kind_ck'
    ) THEN
        ALTER TABLE agent_sessions
            ADD CONSTRAINT agent_sessions_kind_ck CHECK (kind IN ('chat', 'routine'));
    END IF;
END $$;

CREATE SEQUENCE IF NOT EXISTS routine_key_seq;
CREATE SEQUENCE IF NOT EXISTS routine_run_key_seq;

CREATE TABLE IF NOT EXISTS routines (
    id                      TEXT PRIMARY KEY,
    key                     TEXT NOT NULL UNIQUE,
    user_id                 TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assignee_assistant_id   TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    created_by_assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    created_by_session_id   TEXT NOT NULL DEFAULT '',
    issue_id                TEXT REFERENCES issues (id) ON DELETE SET NULL,
    title                   TEXT NOT NULL,
    brief                   TEXT NOT NULL DEFAULT '',
    autonomy                TEXT NOT NULL CHECK (autonomy IN ('read', 'browse')),
    hosts                   JSONB NOT NULL DEFAULT '[]',
    cron                    TEXT NOT NULL,
    timezone                TEXT NOT NULL,
    deliver_im              BOOLEAN NOT NULL DEFAULT FALSE,
    max_duration_sec        INTEGER NOT NULL DEFAULT 900
        CHECK (max_duration_sec BETWEEN 60 AND 3600),
    state                   JSONB NOT NULL DEFAULT '{}',
    status                  TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused', 'archived')),
    next_run_at             TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS routines_due_idx
    ON routines (next_run_at) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS routines_user_idx
    ON routines (user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS routines_assignee_idx
    ON routines (assignee_assistant_id, status);

CREATE TABLE IF NOT EXISTS routine_runs (
    id               TEXT PRIMARY KEY,
    key              TEXT NOT NULL UNIQUE,
    routine_id       TEXT NOT NULL REFERENCES routines (id) ON DELETE CASCADE,
    user_id          TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id     TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    session_id       TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL
        CHECK (status IN (
            'queued', 'running', 'waiting_user',
            'succeeded', 'failed', 'skipped_overlap', 'skipped_stale', 'cancelled'
        )),
    scheduled_at     TIMESTAMPTZ NOT NULL,
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    wait_started_at  TIMESTAMPTZ,
    summary          TEXT NOT NULL DEFAULT '',
    artifacts        JSONB NOT NULL DEFAULT '[]',
    error            TEXT NOT NULL DEFAULT '',
    deliver_error    TEXT NOT NULL DEFAULT '',
    assist_ticket_id TEXT NOT NULL DEFAULT '',
    budget_left_sec  INTEGER,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS routine_runs_routine_idx
    ON routine_runs (routine_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS routine_runs_one_open_idx
    ON routine_runs (routine_id)
    WHERE status IN ('queued', 'running', 'waiting_user');
