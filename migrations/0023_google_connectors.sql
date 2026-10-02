-- Per-user Google OAuth clients and connector grants (Gmail + Calendar read-only).
-- Separate from oauth_providers / user_identities (those are for GitHub/Gitea login).

CREATE TABLE IF NOT EXISTS user_google_oauth_clients (
    user_id       TEXT PRIMARY KEY REFERENCES users (username) ON DELETE CASCADE,
    client_id     TEXT NOT NULL DEFAULT '',
    client_secret TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_connector_grants (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    kind             TEXT NOT NULL DEFAULT 'google',
    subject          TEXT NOT NULL DEFAULT '',
    email            TEXT NOT NULL DEFAULT '',
    access_token     TEXT NOT NULL DEFAULT '',
    refresh_token    TEXT NOT NULL DEFAULT '',
    token_expires_at TIMESTAMPTZ,
    scopes           TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind)
);
CREATE INDEX IF NOT EXISTS user_connector_grants_user_idx ON user_connector_grants (user_id);

CREATE TABLE IF NOT EXISTS connector_oauth_states (
    state        TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    verifier     TEXT NOT NULL DEFAULT '',
    redirect_uri TEXT NOT NULL DEFAULT '',
    redirect_to  TEXT NOT NULL DEFAULT '',
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
