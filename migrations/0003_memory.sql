-- 0003_memory.sql — short-term session memory + long-term pgvector memory

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS memory_short (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_short_session_idx ON memory_short (session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS memory_short_expires_idx ON memory_short (expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS memory_long (
    id                TEXT PRIMARY KEY,
    agent_id          TEXT NOT NULL,
    user_id           TEXT NOT NULL DEFAULT '',
    kind              TEXT NOT NULL CHECK (kind IN ('fact', 'preference', 'episodic')),
    content           TEXT NOT NULL,
    metadata          JSONB NOT NULL DEFAULT '{}',
    source_session_id TEXT,
    embedding         VECTOR(1024),
    importance        SMALLINT NOT NULL DEFAULT 50 CHECK (importance BETWEEN 0 AND 100),
    last_used_at      TIMESTAMPTZ,
    use_count         INTEGER NOT NULL DEFAULT 0,
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_long_agent_importance_idx ON memory_long (agent_id, importance DESC);
CREATE INDEX IF NOT EXISTS memory_long_expires_idx ON memory_long (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS memory_long_user_idx ON memory_long (user_id) WHERE user_id <> '';
CREATE INDEX IF NOT EXISTS memory_long_run_idx ON memory_long (source_session_id)
    WHERE source_session_id IS NOT NULL AND source_session_id <> '';
CREATE INDEX IF NOT EXISTS memory_long_embedding_hnsw_idx ON memory_long
    USING hnsw (embedding vector_cosine_ops);
