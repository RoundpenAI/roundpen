-- Issues Kanban statuses (project board). Authoritative DDL lives in
-- internal/storage/schema/postgres.sql; this file is the historical mirror.
--
-- Mapping: drafting→new, specced→spec_review, planned→triaged,
-- in_progress→running, done→closed, cancelled→cancelled.

UPDATE issues SET status = CASE status
    WHEN 'drafting'    THEN 'new'
    WHEN 'specced'     THEN 'spec_review'
    WHEN 'planned'     THEN 'triaged'
    WHEN 'in_progress' THEN 'running'
    WHEN 'done'        THEN 'closed'
    ELSE status
END
WHERE status IN ('drafting', 'specced', 'planned', 'in_progress', 'done');

ALTER TABLE issues DROP CONSTRAINT IF EXISTS issues_status_check;
ALTER TABLE issues
    ADD CONSTRAINT issues_status_check
    CHECK (status IN (
        'new', 'spec_review', 'needs_discussion', 'triaged', 'claimed',
        'running', 'verified', 'needs_review', 'failed', 'closed', 'cancelled'
    ));

ALTER TABLE issues ALTER COLUMN status SET DEFAULT 'new';

CREATE TABLE IF NOT EXISTS issue_board_scan_state (
    user_id          TEXT PRIMARY KEY REFERENCES users (username) ON DELETE CASCADE,
    last_notified_at TIMESTAMPTZ,
    last_digest_hash TEXT NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
