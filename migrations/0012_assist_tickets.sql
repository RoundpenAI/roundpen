-- Assist tickets: human-in-the-loop requests raised by assistants (not auto on soft deny).
CREATE TABLE IF NOT EXISTS assist_tickets (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id    TEXT NOT NULL REFERENCES assistants (id) ON DELETE CASCADE,
    session_id      TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL DEFAULT 'permission'
        CHECK (kind IN ('permission', 'policy_apply', 'captcha', 'other')),
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'resolved', 'rejected', 'cancelled')),
    title           TEXT NOT NULL DEFAULT '',
    reason          TEXT NOT NULL DEFAULT '',
    context_summary TEXT NOT NULL DEFAULT '',
    ask_human       TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL DEFAULT '{}',
    resolution      TEXT NOT NULL DEFAULT '',
    resolution_note TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS assist_tickets_user_pending_idx
    ON assist_tickets (user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS assist_tickets_assistant_idx
    ON assist_tickets (assistant_id, status, updated_at DESC);

-- Soft-deny audit (no human ticket unless assistant applies).
CREATE TABLE IF NOT EXISTS policy_denials (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id    TEXT NOT NULL REFERENCES assistants (id) ON DELETE CASCADE,
    session_id      TEXT NOT NULL DEFAULT '',
    dimension       TEXT NOT NULL, -- network | directory | capability
    target          TEXT NOT NULL DEFAULT '',
    reason          TEXT NOT NULL DEFAULT '',
    appliable       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS policy_denials_assistant_idx
    ON policy_denials (assistant_id, created_at DESC);
