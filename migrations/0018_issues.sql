-- Issues & tasks: durable work items created in conversation, with versioned spec/plan docs.
-- Short keys (ISS-12 / TSK-34 / DOC-88) come from per-install sequences so humans and the
-- agent can reference work items by name; uuid stays the primary key like other tables.

CREATE SEQUENCE IF NOT EXISTS issue_key_seq;
CREATE SEQUENCE IF NOT EXISTS task_key_seq;
CREATE SEQUENCE IF NOT EXISTS issue_doc_key_seq;

CREATE TABLE IF NOT EXISTS issues (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL,
    summary      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'drafting'
        CHECK (status IN ('drafting', 'specced', 'planned', 'in_progress', 'done', 'cancelled')),
    origin       TEXT NOT NULL DEFAULT 'chat'
        CHECK (origin IN ('chat', 'console')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS issues_user_status_idx ON issues (user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS issues_assistant_idx ON issues (assistant_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS issues_session_idx ON issues (session_id) WHERE session_id <> '';

CREATE TABLE IF NOT EXISTS tasks (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    issue_id     TEXT NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    position     INTEGER NOT NULL DEFAULT 0,
    title        TEXT NOT NULL,
    detail       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'todo'
        CHECK (status IN ('todo', 'in_progress', 'done', 'blocked', 'cancelled')),
    session_id   TEXT NOT NULL DEFAULT '',
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    done_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS tasks_issue_position_idx ON tasks (issue_id, position, created_at);
CREATE INDEX IF NOT EXISTS tasks_user_status_idx ON tasks (user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS tasks_session_idx ON tasks (session_id) WHERE session_id <> '';

CREATE TABLE IF NOT EXISTS issue_docs (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    issue_id     TEXT NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    task_id      TEXT REFERENCES tasks (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('spec', 'plan')),
    version      INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'current', 'superseded')),
    title        TEXT NOT NULL DEFAULT '',
    content_md   TEXT NOT NULL,
    author_type  TEXT NOT NULL DEFAULT 'assistant' CHECK (author_type IN ('user', 'assistant')),
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, kind, version)
);
CREATE INDEX IF NOT EXISTS issue_docs_issue_kind_idx ON issue_docs (issue_id, kind, version DESC);
CREATE INDEX IF NOT EXISTS issue_docs_task_idx ON issue_docs (task_id) WHERE task_id IS NOT NULL;

-- tasks.plan_doc_id -> issue_docs(id): added after both tables exist (circular reference).
-- ADD COLUMN IF NOT EXISTS keeps the embedded schema re-executable on every boot.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS plan_doc_id TEXT REFERENCES issue_docs (id) ON DELETE SET NULL;
