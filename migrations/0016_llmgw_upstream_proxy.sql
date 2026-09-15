-- Per-upstream egress proxy for the LLM gateway (empty = direct / env default).
ALTER TABLE llmgw_upstreams ADD COLUMN IF NOT EXISTS proxy_url TEXT NOT NULL DEFAULT '';
