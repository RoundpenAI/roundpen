-- Agent Projects: durable multi-agent work containers with a coordinator assistant
-- and a local repo path under projects/<slug>/. See
-- docs/superpowers/specs/2026-10-19-agent-projects-design.md.

-- Widen assistants.kind for project coordinators (one per project).
DO $$
BEGIN
    ALTER TABLE assistants DROP CONSTRAINT IF EXISTS assistants_kind_check;
    ALTER TABLE assistants DROP CONSTRAINT IF EXISTS assistants_kind_ck;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'assistants_kind_ck'
    ) THEN
        ALTER TABLE assistants
            ADD CONSTRAINT assistants_kind_ck
            CHECK (kind IN ('user', 'system', 'coordinator'));
    END IF;
END $$;

CREATE SEQUENCE IF NOT EXISTS project_key_seq;

CREATE TABLE IF NOT EXISTS projects (
    id                        TEXT PRIMARY KEY,
    key                       TEXT NOT NULL UNIQUE,
    user_id                   TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    name                      TEXT NOT NULL,
    icon                      TEXT NOT NULL DEFAULT '',
    slug                      TEXT NOT NULL,
    repo_path                 TEXT NOT NULL,
    repo_url                  TEXT NOT NULL DEFAULT '',
    coordinator_assistant_id  TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    coordinator_session_id    TEXT NOT NULL DEFAULT '',
    status                    TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'archived')),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, slug)
);
CREATE INDEX IF NOT EXISTS projects_user_idx
    ON projects (user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS projects_coordinator_idx
    ON projects (coordinator_assistant_id)
    WHERE coordinator_assistant_id IS NOT NULL;
