-- Per-user egress proxy selection per sandbox slot: an admin proxy-profile id
-- (settings.proxies[].id); empty string means direct / env default.
ALTER TABLE users ADD COLUMN IF NOT EXISTS agent_proxy TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS browser_proxy TEXT NOT NULL DEFAULT '';
