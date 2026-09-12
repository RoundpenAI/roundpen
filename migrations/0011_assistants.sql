-- Assistants: user-facing agent profiles (constitution). Sandboxes remain implementation detail.
CREATE TABLE IF NOT EXISTS assistants (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    bio             TEXT NOT NULL DEFAULT '',
    identity_mode   TEXT NOT NULL DEFAULT 'proxy_user'
        CHECK (identity_mode IN ('proxy_user', 'independent')),
    capabilities    JSONB NOT NULL DEFAULT '{"shell":true,"browser":false,"mobile":false,"desktop":false}',
    network_tier    TEXT NOT NULL DEFAULT 'dev_sites'
        CHECK (network_tier IN ('none', 'dev_sites', 'all')),
    network_allowlist JSONB NOT NULL DEFAULT '[]',
    directory_grants  JSONB NOT NULL DEFAULT '[]',
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS assistants_user_idx ON assistants (user_id, updated_at DESC);

ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS agent_sessions_assistant_idx
    ON agent_sessions (assistant_id, updated_at DESC);
