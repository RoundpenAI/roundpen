-- Per-user model source: 'gateway' (default, llmgw env injected into agent
-- sandboxes) or 'own' (env withheld so agents use the user's vendor login).
ALTER TABLE users ADD COLUMN IF NOT EXISTS model_source TEXT NOT NULL DEFAULT 'gateway';
