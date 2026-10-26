-- Proactive recall cooldown / last digest fingerprint per user.
-- Mirrors internal/storage/schema/postgres.sql

CREATE TABLE IF NOT EXISTS proactive_state (
    user_id         TEXT PRIMARY KEY REFERENCES users (username) ON DELETE CASCADE,
    last_notified_at TIMESTAMPTZ,
    last_digest_hash TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
