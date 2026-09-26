-- Per-assistant IM platform bindings (Telegram, Feishu, Weixin, ...).
ALTER TABLE assistants
    ADD COLUMN IF NOT EXISTS im_channels JSONB NOT NULL DEFAULT '{}';
