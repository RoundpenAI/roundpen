-- Slot column for environment image templates (agent|browser|mobile)
ALTER TABLE templates ADD COLUMN IF NOT EXISTS slot TEXT NOT NULL DEFAULT 'agent';
UPDATE templates SET slot = 'browser' WHERE lower(profile) = 'browser' AND (slot = '' OR slot = 'agent');
CREATE INDEX IF NOT EXISTS templates_slot_idx ON templates (slot);

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
