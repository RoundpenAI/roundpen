// Package memory provides short-term and long-term agent memory on PostgreSQL.
// The agent-facing long-term API follows a mem0-inspired shape (add / search /
// get / update / delete) with Roundpen scoping via agent_id / user_id / run_id.
package memory

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a memory row does not exist.
var ErrNotFound = errors.New("not found")

// EmbeddingDims is the pgvector column width for long-term memory embeddings.
const EmbeddingDims = 1024

// ShortEntry is session / working memory (JSONB in Postgres, optional TTL).
type ShortEntry struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	Payload   []byte     `json:"payload"` // JSON
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// LongKind categorises a long-term memory entry.
type LongKind string

const (
	LongFact       LongKind = "fact"
	LongPreference LongKind = "preference"
	LongEpisodic   LongKind = "episodic"
)

// LongEntry is durable structured memory; embedding stored via pgvector.
type LongEntry struct {
	ID              string            `json:"id"`
	AgentID         string            `json:"agent_id"`
	UserID          string            `json:"user_id,omitempty"`
	Kind            LongKind          `json:"kind"`
	Content         string            `json:"content"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	SourceSessionID string            `json:"run_id,omitempty"` // mem0-style run/session scope
	Importance      int               `json:"importance"`
	LastUsedAt      *time.Time        `json:"last_used_at,omitempty"`
	UseCount        int               `json:"use_count"`
	ExpiresAt       *time.Time        `json:"expires_at,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// LongFilter narrows ListLong queries.
type LongFilter struct {
	AgentID  string
	UserID   string
	RunID    string
	Kind     LongKind
	MinScore int
	Limit    int
	Offset   int
}

// SearchFilter scopes vector / keyword search (mem0-style filters object).
type SearchFilter struct {
	AgentID string
	UserID  string
	RunID   string
	Kind    LongKind
}

// ScoredMemory is a search hit with a [0,1] relevance score.
type ScoredMemory struct {
	LongEntry
	Score float64 `json:"score"`
}

// Store persists short- and long-term memory. One Postgres database, separate tables.
type Store interface {
	PutShort(ctx context.Context, e ShortEntry) error
	GetShort(ctx context.Context, id string) (ShortEntry, error)
	ListShort(ctx context.Context, sessionID string) ([]ShortEntry, error)
	DeleteShort(ctx context.Context, id string) error
	DeleteExpiredShort(ctx context.Context, now time.Time) (int64, error)

	PutLong(ctx context.Context, e LongEntry, embedding []float32) error
	GetLong(ctx context.Context, id string) (LongEntry, error)
	ListLong(ctx context.Context, f LongFilter) ([]LongEntry, error)
	CountLong(ctx context.Context, f LongFilter) (int64, error)
	SearchLong(ctx context.Context, query []float32, f SearchFilter, limit int) ([]ScoredMemory, error)
	TouchLong(ctx context.Context, ids []string, at time.Time) error
	DeleteLong(ctx context.Context, id string) error
	PurgeExpiredLong(ctx context.Context, now time.Time) (int64, error)
	ListMissingEmbedding(ctx context.Context, limit int) ([]LongEntry, error)
}

// Embedder generates vectors for memory content / search queries.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
