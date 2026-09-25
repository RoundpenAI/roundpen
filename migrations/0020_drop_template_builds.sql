-- Remove the in-app template build pipeline: artifact columns move onto the
-- catalog row (templates), the build/tag/log tables are dropped, and sandboxes
-- stop pinning a build id.
-- Mirrors internal/storage/schema/postgres.sql (the embedded, authoritative copy).

ALTER TABLE templates ADD COLUMN IF NOT EXISTS artifact_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS base_image TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS cpu_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS memory_mb INTEGER NOT NULL DEFAULT 512;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS disk_size_mb INTEGER NOT NULL DEFAULT 5120;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS envd_version TEXT NOT NULL DEFAULT '0.0.0-roundpen';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS start_cmd TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS snapshot BOOLEAN NOT NULL DEFAULT false;

DO $$
BEGIN
    IF to_regclass('template_tags') IS NOT NULL AND to_regclass('template_builds') IS NOT NULL THEN
        UPDATE templates t SET
            artifact_ref = b.artifact_ref,
            base_image   = b.base_image,
            cpu_count    = b.cpu_count,
            memory_mb    = b.memory_mb,
            disk_size_mb = b.disk_size_mb,
            envd_version = b.envd_version,
            start_cmd    = b.start_cmd,
            snapshot     = b.snapshot
        FROM template_tags tg
        JOIN template_builds b ON b.id = tg.build_id
        WHERE tg.template_id = t.id
          AND tg.tag = 'default'
          AND t.artifact_ref = '';
    END IF;
END $$;

DROP TABLE IF EXISTS template_build_logs;
DROP TABLE IF EXISTS template_tags;
DROP TABLE IF EXISTS template_builds;

ALTER TABLE sandboxes DROP COLUMN IF EXISTS template_build_id;
