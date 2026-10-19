-- Phase 2 long tasks: token budget on routines/runs.

ALTER TABLE routines
    ADD COLUMN IF NOT EXISTS max_tokens INTEGER NOT NULL DEFAULT 0;

ALTER TABLE routine_runs
    ADD COLUMN IF NOT EXISTS tokens_used INTEGER NOT NULL DEFAULT 0;

DO $$
BEGIN
    ALTER TABLE routines DROP CONSTRAINT IF EXISTS routines_max_tokens_ck;
    ALTER TABLE routines
        ADD CONSTRAINT routines_max_tokens_ck
        CHECK (max_tokens >= 0);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE routine_runs DROP CONSTRAINT IF EXISTS routine_runs_tokens_used_ck;
    ALTER TABLE routine_runs
        ADD CONSTRAINT routine_runs_tokens_used_ck
        CHECK (tokens_used >= 0);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;
