-- Browser capability on by default for every assistant. One-shot data flip so
-- later schema replays do not undo an intentional detail-page toggle off.
-- (Authoritative copy lives in internal/storage/schema/postgres.sql.)
ALTER TABLE assistants
    ALTER COLUMN capabilities SET DEFAULT '{"shell":true,"browser":true,"mobile":false,"desktop":false}';
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM schema_migrations WHERE id = 'assistant_browser_default_v1'
    ) THEN
        UPDATE assistants
        SET capabilities = jsonb_set(COALESCE(capabilities, '{}'::jsonb), '{browser}', 'true'::jsonb, true)
        WHERE COALESCE(capabilities->>'browser', 'false') <> 'true';
        INSERT INTO schema_migrations (id, hash, applied_at)
        VALUES ('assistant_browser_default_v1', 'applied', now());
    END IF;
END $$;
