-- Web Push subscriptions for PWA system notifications.
-- Mirrors internal/storage/schema/postgres.sql (the embedded, authoritative copy).

CREATE TABLE IF NOT EXISTS push_subscriptions (
    endpoint    TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    p256dh      TEXT NOT NULL,
    auth        TEXT NOT NULL,
    user_agent  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS push_subscriptions_user_idx
    ON push_subscriptions (user_id);
