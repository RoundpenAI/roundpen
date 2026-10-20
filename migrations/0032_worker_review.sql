-- Structured reviewer output + user Pass / Request-changes decision.
ALTER TABLE project_workers
    ADD COLUMN IF NOT EXISTS review_json TEXT NOT NULL DEFAULT '';

ALTER TABLE project_workers
    ADD COLUMN IF NOT EXISTS user_decision TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    ALTER TABLE project_workers DROP CONSTRAINT IF EXISTS project_workers_user_decision_ck;
    ALTER TABLE project_workers
        ADD CONSTRAINT project_workers_user_decision_ck
        CHECK (user_decision IN ('', 'passed', 'changes_requested'));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;
