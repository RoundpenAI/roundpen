-- Per-user MCP server connections and in-flight OAuth flows.
-- Runtime schema lives in internal/storage/schema/postgres.sql (embedded);
-- this file is the historical archive of the same DDL.

CREATE TABLE IF NOT EXISTS user_mcp_connections (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    server_id        TEXT NOT NULL,
    auth_kind        TEXT NOT NULL DEFAULT 'oauth',      -- oauth | api_key
    status           TEXT NOT NULL DEFAULT 'connected',  -- connected | needs_reauth | error
    account_label    TEXT NOT NULL DEFAULT '',
    api_key          TEXT NOT NULL DEFAULT '',           -- secretbox sealed
    access_token     TEXT NOT NULL DEFAULT '',           -- sealed
    refresh_token    TEXT NOT NULL DEFAULT '',           -- sealed
    token_expires_at TIMESTAMPTZ,
    scopes           TEXT NOT NULL DEFAULT '',
    token_endpoint   TEXT NOT NULL DEFAULT '',
    client_id        TEXT NOT NULL DEFAULT '',
    client_secret    TEXT NOT NULL DEFAULT '',           -- sealed
    resource         TEXT NOT NULL DEFAULT '',
    tools            JSONB NOT NULL DEFAULT '[]'::jsonb,
    tools_synced_at  TIMESTAMPTZ,
    last_error       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, server_id)
);
CREATE INDEX IF NOT EXISTS user_mcp_connections_user_idx ON user_mcp_connections (user_id);

CREATE TABLE IF NOT EXISTS mcp_oauth_states (
    state                  TEXT PRIMARY KEY,
    user_id                TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    server_id              TEXT NOT NULL,
    verifier               TEXT NOT NULL DEFAULT '',
    redirect_uri           TEXT NOT NULL DEFAULT '',
    redirect_to            TEXT NOT NULL DEFAULT '',
    resource               TEXT NOT NULL DEFAULT '',
    authorization_endpoint TEXT NOT NULL,
    token_endpoint         TEXT NOT NULL,
    client_id              TEXT NOT NULL,
    client_secret          TEXT NOT NULL DEFAULT '',
    scopes                 TEXT NOT NULL DEFAULT '',
    expires_at             TIMESTAMPTZ NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
