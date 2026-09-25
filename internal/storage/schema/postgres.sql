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
    -- gateway: sandbox agents call llmgw with a platform virtual key.
    -- own: the gateway env is withheld so agents use the user's own login
    -- (vendor subscription / free tier) inside the sandbox.
    model_source    TEXT NOT NULL DEFAULT 'gateway',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS model_source TEXT NOT NULL DEFAULT 'gateway';

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

-- LLM gateway (virtual keys, request log). Providers live in setting_items
-- (kind 'llm') since the multi-provider rework; llmgw_upstreams is retired.
DROP TABLE IF EXISTS llmgw_upstreams;

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
-- Sandboxes no longer pin a template build: images are built in CI and the
-- catalog row carries the artifact.
ALTER TABLE sandboxes DROP COLUMN IF EXISTS template_build_id;

-- Image catalog. Rows are seeded at boot from ROUNDPEN_AGENT_IMAGE /
-- ROUNDPEN_BROWSER_IMAGE; the in-app registry UI and build pipeline were
-- removed 2026-09-20 and images are built in CI.
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
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    artifact_ref    TEXT NOT NULL DEFAULT '',
    base_image      TEXT NOT NULL DEFAULT '',
    cpu_count       INTEGER NOT NULL DEFAULT 1,
    memory_mb       INTEGER NOT NULL DEFAULT 512,
    disk_size_mb    INTEGER NOT NULL DEFAULT 5120,
    envd_version    TEXT NOT NULL DEFAULT '0.0.0-roundpen',
    start_cmd       TEXT NOT NULL DEFAULT '',
    snapshot        BOOLEAN NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX IF NOT EXISTS templates_namespace_name_uniq
    ON templates (namespace, name);

-- Slot model: agent (OCI) | browser (qcow2) | mobile (reserved)
ALTER TABLE templates ADD COLUMN IF NOT EXISTS slot TEXT NOT NULL DEFAULT 'agent';
UPDATE templates SET slot = 'browser' WHERE lower(profile) = 'browser' AND (slot = '' OR slot = 'agent');
UPDATE templates SET slot = 'agent' WHERE slot IS NULL OR slot = '';
CREATE INDEX IF NOT EXISTS templates_slot_idx ON templates (slot);

-- Artifact columns moved off the removed build tables onto the catalog row.
ALTER TABLE templates ADD COLUMN IF NOT EXISTS artifact_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS base_image TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS cpu_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS memory_mb INTEGER NOT NULL DEFAULT 512;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS disk_size_mb INTEGER NOT NULL DEFAULT 5120;
ALTER TABLE templates ADD COLUMN IF NOT EXISTS envd_version TEXT NOT NULL DEFAULT '0.0.0-roundpen';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS start_cmd TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN IF NOT EXISTS snapshot BOOLEAN NOT NULL DEFAULT false;

-- One-time backfill from the pre-removal default-tag build, then drop the build
-- tables. Guarded so the schema stays replayable once they are gone.
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
    kind            TEXT NOT NULL DEFAULT 'user'
        CHECK (kind IN ('user', 'system')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS assistants_user_idx ON assistants (user_id, updated_at DESC);
ALTER TABLE assistants ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'user';
ALTER TABLE assistants ADD COLUMN IF NOT EXISTS im_channels JSONB NOT NULL DEFAULT '{}';
CREATE UNIQUE INDEX IF NOT EXISTS assistants_user_system_active_idx
    ON assistants (user_id)
    WHERE kind = 'system' AND status = 'active';

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

-- Per-user git tokens (never baked into images). Injected into guest $HOME/.roundpen/git.
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

-- Assist tickets: human-in-the-loop requests raised by assistants (not auto on soft deny).
CREATE TABLE IF NOT EXISTS assist_tickets (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id    TEXT NOT NULL REFERENCES assistants (id) ON DELETE CASCADE,
    session_id      TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL DEFAULT 'permission'
        CHECK (kind IN ('permission', 'policy_apply', 'captcha', 'other')),
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'resolved', 'rejected', 'cancelled')),
    title           TEXT NOT NULL DEFAULT '',
    reason          TEXT NOT NULL DEFAULT '',
    context_summary TEXT NOT NULL DEFAULT '',
    ask_human       TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL DEFAULT '{}',
    resolution      TEXT NOT NULL DEFAULT '',
    resolution_note TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS assist_tickets_user_pending_idx
    ON assist_tickets (user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS assist_tickets_assistant_idx
    ON assist_tickets (assistant_id, status, updated_at DESC);

-- Soft-deny audit (no human ticket unless assistant applies).
CREATE TABLE IF NOT EXISTS policy_denials (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id    TEXT NOT NULL REFERENCES assistants (id) ON DELETE CASCADE,
    session_id      TEXT NOT NULL DEFAULT '',
    dimension       TEXT NOT NULL,
    target          TEXT NOT NULL DEFAULT '',
    reason          TEXT NOT NULL DEFAULT '',
    appliable       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS policy_denials_assistant_idx
    ON policy_denials (assistant_id, created_at DESC);

-- Issues & tasks: durable work items created in conversation, with versioned spec/plan docs.
-- Short keys (ISS-12 / TSK-34 / DOC-88) come from per-install sequences so humans and the
-- agent can reference work items by name; uuid stays the primary key like other tables.

CREATE SEQUENCE IF NOT EXISTS issue_key_seq;
CREATE SEQUENCE IF NOT EXISTS task_key_seq;
CREATE SEQUENCE IF NOT EXISTS issue_doc_key_seq;

CREATE TABLE IF NOT EXISTS issues (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL,
    summary      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'drafting'
        CHECK (status IN ('drafting', 'specced', 'planned', 'in_progress', 'done', 'cancelled')),
    origin       TEXT NOT NULL DEFAULT 'chat'
        CHECK (origin IN ('chat', 'console')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS issues_user_status_idx ON issues (user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS issues_assistant_idx ON issues (assistant_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS issues_session_idx ON issues (session_id) WHERE session_id <> '';

CREATE TABLE IF NOT EXISTS tasks (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    issue_id     TEXT NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    position     INTEGER NOT NULL DEFAULT 0,
    title        TEXT NOT NULL,
    detail       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'todo'
        CHECK (status IN ('todo', 'in_progress', 'done', 'blocked', 'cancelled')),
    session_id   TEXT NOT NULL DEFAULT '',
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    done_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS tasks_issue_position_idx ON tasks (issue_id, position, created_at);
CREATE INDEX IF NOT EXISTS tasks_user_status_idx ON tasks (user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS tasks_session_idx ON tasks (session_id) WHERE session_id <> '';

CREATE TABLE IF NOT EXISTS issue_docs (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    issue_id     TEXT NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    task_id      TEXT REFERENCES tasks (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('spec', 'plan')),
    version      INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'current', 'superseded')),
    title        TEXT NOT NULL DEFAULT '',
    content_md   TEXT NOT NULL,
    author_type  TEXT NOT NULL DEFAULT 'assistant' CHECK (author_type IN ('user', 'assistant')),
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, kind, version)
);
CREATE INDEX IF NOT EXISTS issue_docs_issue_kind_idx ON issue_docs (issue_id, kind, version DESC);
CREATE INDEX IF NOT EXISTS issue_docs_task_idx ON issue_docs (task_id) WHERE task_id IS NOT NULL;

-- tasks.plan_doc_id -> issue_docs(id): added after both tables exist (circular reference).
-- ADD COLUMN IF NOT EXISTS keeps the embedded schema re-executable on every boot.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS plan_doc_id TEXT REFERENCES issue_docs (id) ON DELETE SET NULL;


-- OAuth2 federated login (GitHub / self-hosted Gitea).
-- oauth_providers: one row per remote OAuth app (a Gitea instance = one row).
CREATE TABLE IF NOT EXISTS oauth_providers (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL DEFAULT 'gitea',
    scheme        TEXT NOT NULL DEFAULT 'https',
    host          TEXT NOT NULL,
    label         TEXT NOT NULL DEFAULT '',
    client_id     TEXT NOT NULL DEFAULT '',
    client_secret TEXT NOT NULL DEFAULT '',
    scopes        TEXT NOT NULL DEFAULT '',
    auth_url      TEXT NOT NULL DEFAULT '',
    token_url     TEXT NOT NULL DEFAULT '',
    api_url       TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, host)
);

-- user_identities: remote account linked to a local user; holds the tokens that
-- are projected into the guest git credentials (a manual PAT wins per host).
CREATE TABLE IF NOT EXISTS user_identities (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    provider_id      TEXT NOT NULL REFERENCES oauth_providers (id) ON DELETE CASCADE,
    subject          TEXT NOT NULL,
    login            TEXT NOT NULL DEFAULT '',
    name             TEXT NOT NULL DEFAULT '',
    email            TEXT NOT NULL DEFAULT '',
    access_token     TEXT NOT NULL DEFAULT '',
    refresh_token    TEXT NOT NULL DEFAULT '',
    token_expires_at TIMESTAMPTZ,
    scopes           TEXT NOT NULL DEFAULT '',
    last_login_at    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_id, subject),
    UNIQUE (user_id, provider_id)
);
CREATE INDEX IF NOT EXISTS user_identities_user_idx ON user_identities (user_id);

-- oauth_states: single-use authorization state (PKCE verifier + link-flow owner).
CREATE TABLE IF NOT EXISTS oauth_states (
    state       TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    verifier    TEXT NOT NULL DEFAULT '',
    link_user    TEXT NOT NULL DEFAULT '',
    redirect_uri TEXT NOT NULL DEFAULT '',
    redirect_to  TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- setting_items: named, typed configuration entries ("items") grouped by kind.
-- Usage sites ("slots") select one item via setting_bindings. Secret config
-- values are sealed (AES-GCM "enc:v1:") in their own column; config holds only
-- non-secret fields.
CREATE TABLE IF NOT EXISTS setting_items (
    kind        TEXT NOT NULL,
    id          TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    position    INTEGER NOT NULL DEFAULT 0,
    config      JSONB NOT NULL DEFAULT '{}',
    secrets     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, id)
);
CREATE INDEX IF NOT EXISTS setting_items_kind_pos_idx ON setting_items (kind, position, id);

-- setting_bindings: which item a slot resolves to, per scope. scope is 'global'
-- (admin default) or 'user:<username>' (personal override); item_id '' means
-- inherit. Reference integrity is enforced in Go: deleting an item also
-- deletes the bindings that pointed at it.
CREATE TABLE IF NOT EXISTS setting_bindings (
    scope      TEXT NOT NULL,
    slot       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    item_id    TEXT NOT NULL DEFAULT '',
    params     JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, slot),
    CONSTRAINT setting_bindings_scope_ck CHECK (scope = 'global' OR scope LIKE 'user:%')
);
CREATE INDEX IF NOT EXISTS setting_bindings_item_idx ON setting_bindings (kind, item_id);

-- Per-user egress-proxy selections moved into setting_bindings. The copy runs
-- once, while the columns still exist; a value that no longer matches an item
-- id simply resolves to "no selection".
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'users' AND column_name = 'agent_proxy') THEN
        INSERT INTO setting_bindings (scope, slot, kind, item_id)
        SELECT 'user:' || username, 'proxy.agent', 'proxy', agent_proxy
        FROM users WHERE coalesce(agent_proxy, '') <> ''
        ON CONFLICT (scope, slot) DO NOTHING;
        INSERT INTO setting_bindings (scope, slot, kind, item_id)
        SELECT 'user:' || username, 'proxy.browser', 'proxy', browser_proxy
        FROM users WHERE coalesce(browser_proxy, '') <> ''
        ON CONFLICT (scope, slot) DO NOTHING;
        ALTER TABLE users DROP COLUMN agent_proxy, DROP COLUMN browser_proxy;
    END IF;
END $$;
