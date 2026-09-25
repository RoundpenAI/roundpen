-- OAuth2 federated login (GitHub / self-hosted Gitea).
-- Mirrors internal/storage/schema/postgres.sql (the embedded, authoritative copy).

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
