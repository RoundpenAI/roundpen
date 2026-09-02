package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service is the agent-facing memory facade (embed + store).
type Service struct {
	Store  Store
	Embed  Embedder // optional; when nil, vectors are skipped
	Logger *slog.Logger
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// AddInput is the mem0-inspired add payload.
type AddInput struct {
	Text       string    // explicit memory text
	Messages   []Message // conversation turns (joined when Text empty)
	AgentID    string    // required
	UserID     string
	RunID      string
	Kind       LongKind
	Metadata   map[string]string
	Importance int
	ExpiresAt  *time.Time
	Embedding  []float32 // optional override; skips auto-embed when set
	Infer      bool      // reserved; currently stores concatenated content
}

// Message is one chat turn for add-from-conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SearchInput is the mem0-inspired search payload.
type SearchInput struct {
	Query     string
	Filters   SearchFilter
	TopK      int
	Threshold float64 // min score; 0 disables
	Embedding []float32
}

// Add creates a long-term memory and auto-embeds when an Embedder is configured.
func (s *Service) Add(ctx context.Context, in AddInput) (LongEntry, error) {
	content := strings.TrimSpace(in.Text)
	if content == "" {
		content = contentFromMessages(in.Messages)
	}
	if content == "" {
		return LongEntry{}, fmt.Errorf("text or messages required")
	}
	if strings.TrimSpace(in.AgentID) == "" {
		return LongEntry{}, fmt.Errorf("agent_id is required")
	}
	kind := in.Kind
	if kind == "" {
		kind = LongFact
	}
	imp := in.Importance
	if imp == 0 {
		imp = 50
	}
	e := LongEntry{
		ID:              uuid.NewString(),
		AgentID:         in.AgentID,
		UserID:          in.UserID,
		Kind:            kind,
		Content:         content,
		Metadata:        in.Metadata,
		SourceSessionID: in.RunID,
		Importance:      imp,
		ExpiresAt:       in.ExpiresAt,
		CreatedAt:       time.Now().UTC(),
	}

	emb := in.Embedding
	if len(emb) == 0 && s.Embed != nil {
		vecs, err := s.Embed.Embed(ctx, []string{content})
		if err != nil {
			s.log().Warn("memory embed on add failed; storing without vector", slog.Any("err", err))
		} else if len(vecs) > 0 {
			emb = vecs[0]
		}
	}
	if err := s.Store.PutLong(ctx, e, emb); err != nil {
		return LongEntry{}, err
	}
	return e, nil
}

// Search embeds the query (when needed) and returns scored memories.
func (s *Service) Search(ctx context.Context, in SearchInput) ([]ScoredMemory, error) {
	if strings.TrimSpace(in.Query) == "" && len(in.Embedding) == 0 {
		return nil, fmt.Errorf("query is required")
	}
	if in.Filters.AgentID == "" && in.Filters.UserID == "" && in.Filters.RunID == "" {
		return nil, fmt.Errorf("filters must include agent_id, user_id, or run_id")
	}
	topK := in.TopK
	if topK <= 0 {
		topK = 10
	}

	emb := in.Embedding
	if len(emb) == 0 && s.Embed != nil {
		vecs, err := s.Embed.Embed(ctx, []string{in.Query})
		if err != nil {
			return nil, fmt.Errorf("embed query: %w", err)
		}
		if len(vecs) > 0 {
			emb = vecs[0]
		}
	}

	results, err := s.Store.SearchLong(ctx, emb, in.Filters, topK)
	if err != nil {
		return nil, err
	}
	if in.Threshold > 0 {
		filtered := results[:0]
		for _, r := range results {
			if r.Score >= in.Threshold {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.ID)
	}
	_ = s.Store.TouchLong(ctx, ids, time.Now().UTC())
	return results, nil
}

// Update patches a memory and re-embeds when content changes.
func (s *Service) Update(ctx context.Context, id string, patch UpdateInput) (LongEntry, error) {
	e, err := s.Store.GetLong(ctx, id)
	if err != nil {
		return LongEntry{}, err
	}
	contentChanged := false
	if patch.Content != nil && *patch.Content != e.Content {
		e.Content = *patch.Content
		contentChanged = true
	}
	if patch.Kind != nil {
		e.Kind = *patch.Kind
	}
	if patch.Metadata != nil {
		e.Metadata = patch.Metadata
	}
	if patch.Importance != nil {
		e.Importance = *patch.Importance
	}
	if patch.ExpiresAt != nil {
		e.ExpiresAt = patch.ExpiresAt
	}
	if patch.UserID != nil {
		e.UserID = *patch.UserID
	}
	if patch.RunID != nil {
		e.SourceSessionID = *patch.RunID
	}

	var emb []float32
	if len(patch.Embedding) > 0 {
		emb = patch.Embedding
	} else if contentChanged && s.Embed != nil {
		vecs, err := s.Embed.Embed(ctx, []string{e.Content})
		if err != nil {
			s.log().Warn("memory embed on update failed", slog.Any("err", err))
		} else if len(vecs) > 0 {
			emb = vecs[0]
		}
	}
	if err := s.Store.PutLong(ctx, e, emb); err != nil {
		return LongEntry{}, err
	}
	return e, nil
}

// UpdateInput is a partial patch for Update.
type UpdateInput struct {
	Content    *string
	Kind       *LongKind
	Metadata   map[string]string
	Importance *int
	ExpiresAt  *time.Time
	UserID     *string
	RunID      *string
	Embedding  []float32
}

func contentFromMessages(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		c := strings.TrimSpace(m.Content)
		if c == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		role := m.Role
		if role == "" {
			role = "user"
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(c)
	}
	return b.String()
}

// RunReembed backfills missing embeddings.
func RunReembed(ctx context.Context, store Store, embed Embedder, logger *slog.Logger, interval time.Duration) {
	if embed == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			missing, err := store.ListMissingEmbedding(ctx, 50)
			if err != nil {
				logger.Error("memory reembed list", slog.Any("err", err))
				continue
			}
			for _, e := range missing {
				vecs, err := embed.Embed(ctx, []string{e.Content})
				if err != nil {
					logger.Warn("memory reembed failed", slog.String("id", e.ID), slog.Any("err", err))
					continue
				}
				if len(vecs) == 0 {
					continue
				}
				if err := store.PutLong(ctx, e, vecs[0]); err != nil {
					logger.Warn("memory reembed store", slog.String("id", e.ID), slog.Any("err", err))
				}
			}
			if len(missing) > 0 {
				logger.Info("memory reembed pass", slog.Int("attempted", len(missing)))
			}
		}
	}
}
