-- Code-dev worker roles: implementer | reviewer | tester.
ALTER TABLE project_workers
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'implementer';

DO $$
BEGIN
    ALTER TABLE project_workers DROP CONSTRAINT IF EXISTS project_workers_role_ck;
    ALTER TABLE project_workers
        ADD CONSTRAINT project_workers_role_ck
        CHECK (role IN ('implementer', 'reviewer', 'tester'));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;
