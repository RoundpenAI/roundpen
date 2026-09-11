-- Roundpen Phase 1 schema

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Users + cookie sessions (password login and per-user API keys)
CREATE TABLE IF NOT EXISTS users (
    username        TEXT PRIMARY KEY,
    email           TEXT NOT NULL DEFAULT '',
    fullname        TEXT NOT NULL DEFAULT '',
    org_name        TEXT NOT NULL DEFAULT '',
    api_key         TEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT 'user',
    password_hash   TEXT,
    auth_provider   TEXT NOT NULL DEFAULT 'local',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_api_key ON users (api_key);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email
    ON users (lower(email))
    WHERE email <> '';

CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_agent   TEXT,
    ip           TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions (expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_sessions_token_hash ON sessions (token_hash);

CREATE TABLE IF NOT EXISTS sandboxes (
    id              TEXT PRIMARY KEY,
    container_id    TEXT NOT NULL DEFAULT '',
    image           TEXT NOT NULL,
    status          TEXT NOT NULL,
    workspace_id    TEXT NOT NULL DEFAULT '',
    workspace_path  TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}',
    ttl_seconds     INTEGER NOT NULL DEFAULT 1800,
    expires_at      TIMESTAMPTZ,
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

-- Human-readable label (added after initial MVP schema).
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
-- Free-form category for agent resolution (e.g. Browser, Code).
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT '';
-- At most one default sandbox per non-empty category.
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS sandboxes_status_idx ON sandboxes (status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS sandboxes_expires_at_idx ON sandboxes (expires_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS sandboxes_category_idx ON sandboxes (lower(category)) WHERE deleted_at IS NULL AND category <> '';
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT '';
UPDATE sandboxes SET owner = 'admin' WHERE owner = '';
CREATE INDEX IF NOT EXISTS sandboxes_owner_idx ON sandboxes (owner) WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS sandboxes_name_uniq;
CREATE UNIQUE INDEX IF NOT EXISTS sandboxes_owner_name_uniq
    ON sandboxes (owner, lower(name))
    WHERE deleted_at IS NULL AND name <> '';
DROP INDEX IF EXISTS sandboxes_category_default_uniq;
CREATE UNIQUE INDEX IF NOT EXISTS sandboxes_owner_category_default_uniq
    ON sandboxes (owner, lower(category))
    WHERE deleted_at IS NULL AND is_default AND category <> '';

-- LLM gateway (virtual keys, upstream vault, request log)
CREATE TABLE IF NOT EXISTS llmgw_upstreams (
    provider        TEXT PRIMARY KEY,
    base_url        TEXT NOT NULL,
    api_key         TEXT NOT NULL,
    model_map       JSONB NOT NULL DEFAULT '{}',
    model_patterns  JSONB NOT NULL DEFAULT '[]',
    enabled         BOOLEAN NOT NULL DEFAULT true,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS llmgw_virtual_keys (
    key             TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS llmgw_transactions (
    id                          BIGSERIAL PRIMARY KEY,
    request_id                  TEXT NOT NULL,
    virtual_key                 TEXT NOT NULL DEFAULT '',
    virtual_name                TEXT NOT NULL DEFAULT '',
    provider                    TEXT NOT NULL,
    method                      TEXT NOT NULL,
    path                        TEXT NOT NULL,
    upstream_url                TEXT NOT NULL DEFAULT '',
    status_code                 INTEGER NOT NULL,
    request_bytes               BIGINT NOT NULL DEFAULT 0,
    response_bytes              BIGINT NOT NULL DEFAULT 0,
    duration_ms                 BIGINT NOT NULL DEFAULT 0,
    error                       TEXT NOT NULL DEFAULT '',
    request_body                TEXT,
    response_body               TEXT,
    upstream_request_body       TEXT,
    request_truncated           BOOLEAN NOT NULL DEFAULT false,
    response_truncated          BOOLEAN NOT NULL DEFAULT false,
    upstream_request_truncated  BOOLEAN NOT NULL DEFAULT false,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS llmgw_tx_created_idx ON llmgw_transactions (created_at DESC);
CREATE INDEX IF NOT EXISTS llmgw_tx_vk_idx ON llmgw_transactions (virtual_key, created_at DESC);
CREATE INDEX IF NOT EXISTS llmgw_tx_provider_idx ON llmgw_transactions (provider, created_at DESC);

-- Agent memory (short-term session JSONB + long-term pgvector)
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS memory_short (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_short_session_idx ON memory_short (session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS memory_short_expires_idx ON memory_short (expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS memory_long (
    id                TEXT PRIMARY KEY,
    agent_id          TEXT NOT NULL,
    user_id           TEXT NOT NULL DEFAULT '',
    kind              TEXT NOT NULL CHECK (kind IN ('fact', 'preference', 'episodic')),
    content           TEXT NOT NULL,
    metadata          JSONB NOT NULL DEFAULT '{}',
    source_session_id TEXT,
    embedding         VECTOR(1024),
    importance        SMALLINT NOT NULL DEFAULT 50 CHECK (importance BETWEEN 0 AND 100),
    last_used_at      TIMESTAMPTZ,
    use_count         INTEGER NOT NULL DEFAULT 0,
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_long_agent_importance_idx ON memory_long (agent_id, importance DESC);
CREATE INDEX IF NOT EXISTS memory_long_expires_idx ON memory_long (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS memory_long_user_idx ON memory_long (user_id) WHERE user_id <> '';
CREATE INDEX IF NOT EXISTS memory_long_run_idx ON memory_long (source_session_id)
    WHERE source_session_id IS NOT NULL AND source_session_id <> '';
CREATE INDEX IF NOT EXISTS memory_long_embedding_hnsw_idx ON memory_long
    USING hnsw (embedding vector_cosine_ops);

-- Upgrade paths for DBs that already had memory_long without user_id/updated_at
ALTER TABLE memory_long ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_long ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS memory_long_user_idx ON memory_long (user_id) WHERE user_id <> '';
CREATE INDEX IF NOT EXISTS memory_long_run_idx ON memory_long (source_session_id)
    WHERE source_session_id IS NOT NULL AND source_session_id <> '';

-- Sandbox resource limits (from resolved template at create time)
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS cpu_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS memory_mb INTEGER NOT NULL DEFAULT 512;
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS disk_size_mb INTEGER NOT NULL DEFAULT 5120;
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS template_build_id TEXT NOT NULL DEFAULT '';

-- Template registry (T0: static builds; T1+ adds build pipeline)
CREATE TABLE IF NOT EXISTS templates (
    id              TEXT PRIMARY KEY,
    namespace       TEXT NOT NULL DEFAULT 'default',
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    profile         TEXT NOT NULL DEFAULT 'dev',
    slot            TEXT NOT NULL DEFAULT 'agent',
    public          BOOLEAN NOT NULL DEFAULT false,
    spawn_count     BIGINT NOT NULL DEFAULT 0,
    build_count     INTEGER NOT NULL DEFAULT 1,
    last_spawned_at TIMESTAMPTZ,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS templates_namespace_name_uniq
    ON templates (namespace, name);

-- Slot model: agent (OCI) | browser (qcow2) | mobile (reserved)
ALTER TABLE templates ADD COLUMN IF NOT EXISTS slot TEXT NOT NULL DEFAULT 'agent';
UPDATE templates SET slot = 'browser' WHERE lower(profile) = 'browser' AND (slot = '' OR slot = 'agent');
UPDATE templates SET slot = 'agent' WHERE slot IS NULL OR slot = '';
CREATE INDEX IF NOT EXISTS templates_slot_idx ON templates (slot);

CREATE TABLE IF NOT EXISTS template_builds (
    id              TEXT PRIMARY KEY,
    template_id     TEXT NOT NULL REFERENCES templates (id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'ready',
    base_image      TEXT NOT NULL DEFAULT '',
    artifact_ref    TEXT NOT NULL,
    cpu_count       INTEGER NOT NULL DEFAULT 1,
    memory_mb       INTEGER NOT NULL DEFAULT 512,
    disk_size_mb    INTEGER NOT NULL DEFAULT 5120,
    envd_version    TEXT NOT NULL DEFAULT '0.0.0-roundpen',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS template_builds_template_idx ON template_builds (template_id, created_at DESC);

CREATE TABLE IF NOT EXISTS template_tags (
    template_id     TEXT NOT NULL REFERENCES templates (id) ON DELETE CASCADE,
    tag             TEXT NOT NULL DEFAULT 'default',
    build_id        TEXT NOT NULL REFERENCES template_builds (id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (template_id, tag)
);
CREATE INDEX IF NOT EXISTS template_tags_build_idx ON template_tags (build_id);

-- T1/T2: build spec, cache, snapshot metadata
ALTER TABLE template_builds ADD COLUMN IF NOT EXISTS cache_key TEXT NOT NULL DEFAULT '';
ALTER TABLE template_builds ADD COLUMN IF NOT EXISTS spec_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE template_builds ADD COLUMN IF NOT EXISTS layers_json JSONB NOT NULL DEFAULT '[]';
ALTER TABLE template_builds ADD COLUMN IF NOT EXISTS start_cmd TEXT NOT NULL DEFAULT '';
ALTER TABLE template_builds ADD COLUMN IF NOT EXISTS ready_cmd TEXT NOT NULL DEFAULT '';
ALTER TABLE template_builds ADD COLUMN IF NOT EXISTS snapshot BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE template_builds ADD COLUMN IF NOT EXISTS error_message TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS template_builds_cache_key_idx ON template_builds (template_id, cache_key)
    WHERE status = 'ready' AND cache_key <> '';

CREATE TABLE IF NOT EXISTS template_build_logs (
    id          BIGSERIAL PRIMARY KEY,
    build_id    TEXT NOT NULL REFERENCES template_builds (id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    logged_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    level       TEXT NOT NULL DEFAULT 'info',
    message     TEXT NOT NULL,
    step        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS template_build_logs_build_idx ON template_build_logs (build_id, seq);

-- Mutable app settings (admin UI; env seeds on first boot)
CREATE TABLE IF NOT EXISTS app_settings (
    id          TEXT PRIMARY KEY DEFAULT 'global',
    payload     JSONB NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Agent Web UI sessions (ACP gateway)
CREATE TABLE IF NOT EXISTS agent_sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    title        TEXT NOT NULL DEFAULT '',
    provider_id  TEXT NOT NULL DEFAULT 'mock',
    sandbox_id   TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS agent_sessions_user_idx ON agent_sessions (user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS agent_messages (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES agent_sessions (id) ON DELETE CASCADE,
    role         TEXT NOT NULL,
    content      TEXT NOT NULL DEFAULT '',
    meta         JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS agent_messages_session_idx ON agent_messages (session_id, created_at);

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

-- Fixed per-user environment slots (one machine per slot)
CREATE TABLE IF NOT EXISTS user_environments (
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    slot         TEXT NOT NULL,
    sandbox_id   TEXT NOT NULL,
    template_id  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, slot)
);
CREATE INDEX IF NOT EXISTS user_environments_sandbox_idx ON user_environments (sandbox_id);

-- Browser explore / verify tasks (backed by an agent session)
CREATE TABLE IF NOT EXISTS browser_tasks (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    url          TEXT NOT NULL,
    brief        TEXT NOT NULL DEFAULT '',
    session_id   TEXT REFERENCES agent_sessions (id) ON DELETE SET NULL,
    status       TEXT NOT NULL DEFAULT 'open',
    prompt       TEXT NOT NULL DEFAULT '',
    report       JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS browser_tasks_user_idx ON browser_tasks (user_id, created_at DESC);

-- Per-user git tokens (never baked into images). Injected into /workspace/.roundpen/git.
CREATE TABLE IF NOT EXISTS user_git_credentials (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    provider     TEXT NOT NULL DEFAULT 'gitea',
    host         TEXT NOT NULL,
    username     TEXT NOT NULL DEFAULT '',
    label        TEXT NOT NULL DEFAULT '',
    token        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, host)
);
CREATE INDEX IF NOT EXISTS user_git_credentials_user_idx ON user_git_credentials (user_id, host);

-- Per-user agent engine preference (qemu | docker | kern)
CREATE TABLE IF NOT EXISTS user_runtime (
    user_id       TEXT PRIMARY KEY REFERENCES users (username) ON DELETE CASCADE,
    agent_engine  TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

