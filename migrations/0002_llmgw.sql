-- 0002_llmgw.sql — LLM gateway tables (also folded into embedded schema)

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
