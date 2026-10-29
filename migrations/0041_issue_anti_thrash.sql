-- Anti-thrash fields + transition audit for Issues Kanban.
-- Mirrors internal/storage/schema/postgres.sql

ALTER TABLE issues ADD COLUMN IF NOT EXISTS discussion_round INTEGER NOT NULL DEFAULT 0;
ALTER TABLE issues ADD COLUMN IF NOT EXISTS review_round INTEGER NOT NULL DEFAULT 0;
ALTER TABLE issues ADD COLUMN IF NOT EXISTS claim_owner TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE issues ADD COLUMN IF NOT EXISTS last_status_from TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS last_evidence TEXT NOT NULL DEFAULT '';
ALTER TABLE issues ADD COLUMN IF NOT EXISTS last_board_scan_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS issue_transitions (
    id           TEXT PRIMARY KEY,
    issue_id     TEXT NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    from_status  TEXT NOT NULL,
    to_status    TEXT NOT NULL,
    actor_kind   TEXT NOT NULL DEFAULT 'human'
        CHECK (actor_kind IN ('human', 'agent', 'sysadmin', 'system')),
    actor_id     TEXT NOT NULL DEFAULT '',
    reason       TEXT NOT NULL DEFAULT '',
    evidence     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS issue_transitions_issue_idx
    ON issue_transitions (issue_id, created_at DESC);
