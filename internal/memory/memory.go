// Package memory provides short-term and long-term agent memory on PostgreSQL.
package memory

import (
	"context"
	"time"
)

// ShortEntry is session / working memory (JSONB in Postgres).
type ShortEntry struct {
	ID        string
	SessionID string
	Payload   []byte // JSON
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// LongEntry is durable structured memory; embedding stored via pgvector.
type LongEntry struct {
	ID        string
	AgentID   string
	Content   string
	Metadata  map[string]string
	CreatedAt time.Time
}

// Store persists short- and long-term memory. One Postgres database, separate tables.
type Store interface {
	PutShort(ctx context.Context, e ShortEntry) error
	ListShort(ctx context.Context, sessionID string) ([]ShortEntry, error)
	DeleteExpiredShort(ctx context.Context, now time.Time) (int64, error)

	PutLong(ctx context.Context, e LongEntry, embedding []float32) error
	SearchLong(ctx context.Context, agentID string, query []float32, limit int) ([]LongEntry, error)
}
