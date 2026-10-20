-- Grants issued by the shared OAuth relay record source = 'relay'.
-- Empty source is a grant from the user's own Google client.

ALTER TABLE user_connector_grants
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '';
