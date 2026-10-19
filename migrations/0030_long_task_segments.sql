-- Long-running work is done as chained routine segments (≤2h each), not one
-- multi-hour Prompt. Widen max_duration to match the chat turn ceiling and
-- track segment identity for continue chains.

DO $$
BEGIN
    ALTER TABLE routines DROP CONSTRAINT IF EXISTS routines_max_duration_sec_check;
    ALTER TABLE routines DROP CONSTRAINT IF EXISTS routines_max_duration_sec_ck;
EXCEPTION
    WHEN undefined_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE routines
        ADD CONSTRAINT routines_max_duration_sec_ck
        CHECK (max_duration_sec BETWEEN 60 AND 7200);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

ALTER TABLE routine_runs
    ADD COLUMN IF NOT EXISTS segment_index INTEGER NOT NULL DEFAULT 1;
ALTER TABLE routine_runs
    ADD COLUMN IF NOT EXISTS continue_from TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    ALTER TABLE routine_runs DROP CONSTRAINT IF EXISTS routine_runs_segment_index_ck;
    ALTER TABLE routine_runs
        ADD CONSTRAINT routine_runs_segment_index_ck
        CHECK (segment_index >= 1 AND segment_index <= 48);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;
