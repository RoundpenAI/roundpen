-- System assistant kind: one undeletable assistant per user.
ALTER TABLE assistants ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'user';

CREATE UNIQUE INDEX IF NOT EXISTS assistants_user_system_active_idx
    ON assistants (user_id)
    WHERE kind = 'system' AND status = 'active';
