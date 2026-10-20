-- Personal profile fields so Agents can address the user appropriately.
ALTER TABLE users ADD COLUMN IF NOT EXISTS about TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT '';
