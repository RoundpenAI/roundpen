-- 0004_memory_user_id.sql

ALTER TABLE memory_long ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_long ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS memory_long_user_idx ON memory_long (user_id) WHERE user_id <> '';
CREATE INDEX IF NOT EXISTS memory_long_run_idx ON memory_long (source_session_id)
    WHERE source_session_id IS NOT NULL AND source_session_id <> '';
