-- Roundpen Phase 1 schema

CREATE EXTENSION IF NOT EXISTS pgcrypto;

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

CREATE INDEX IF NOT EXISTS sandboxes_status_idx ON sandboxes (status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS sandboxes_expires_at_idx ON sandboxes (expires_at) WHERE deleted_at IS NULL;

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

