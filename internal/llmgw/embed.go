package llmgw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Well-known internal credentials used by Roundpen control-plane services
// (memory embedder, future toolgw, etc.). Agents should not use this key.
const (
	InternalVirtualKey  = "vk-roundpen-internal"
	InternalVirtualName = "roundpen-internal"

	// EmbeddingModelAlias is the client-facing model name for embeddings.
	// Seeded into openai.model_map → real upstream embedding model.
	EmbeddingModelAlias = "roundpen-embed"

	// DefaultEmbeddingModel is the upstream OpenAI-compatible embedding model.
	DefaultEmbeddingModel = "text-embedding-3-small"

	// EmbeddingDimensions matches memory.EmbeddingDims (VECTOR(1024)).
	EmbeddingDimensions = 1024
)

// EnsureInternal seeds the Roundpen-internal virtual key and the
// roundpen-embed → embeddingModel alias on the OpenAI upstream (when present).
func (g *Gateway) EnsureInternal(ctx context.Context, embeddingModel string) error {
	if embeddingModel == "" {
		embeddingModel = DefaultEmbeddingModel
	}
	now := time.Now().UTC()

	if err := g.store.UpsertVirtualKey(VirtualKey{
		Key:       InternalVirtualKey,
		Name:      InternalVirtualName,
		Enabled:   true,
		CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("seed internal virtual key: %w", err)
	}

	u, err := g.store.GetUpstream(ctx, ProviderOpenAI)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil // openai not configured yet
		}
		return err
	}
	if u.ModelMap == nil {
		u.ModelMap = map[string]string{}
	}
	u.ModelMap[EmbeddingModelAlias] = embeddingModel
	u.UpdatedAt = now
	if err := g.store.UpsertUpstream(*u); err != nil {
		return fmt.Errorf("seed embedding model alias: %w", err)
	}
	return nil
}

type embedRequest struct {
	Model      string `json:"model"`
	Input      any    `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// Embed generates embeddings via the OpenAI upstream using the roundpen-embed
// alias (mapped to the configured embedding model). texts must be non-empty.
func (g *Gateway) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("llmgw: embed: empty input")
	}
	u, err := g.store.GetUpstream(ctx, ProviderOpenAI)
	if err != nil {
		return nil, fmt.Errorf("llmgw: embed: openai upstream: %w", err)
	}
	matcher, err := NewModelMatcher(u.ModelMap, u.ModelPatterns)
	if err != nil {
		return nil, err
	}
	model := EmbeddingModelAlias
	if mapped, ok := matcher.Map(model); ok {
		model = mapped
	} else if target, ok := u.ModelMap[EmbeddingModelAlias]; ok && target != "" {
		model = target
	} else {
		model = DefaultEmbeddingModel
	}

	payload, err := json.Marshal(embedRequest{
		Model:      model,
		Input:      texts,
		Dimensions: EmbeddingDimensions,
	})
	if err != nil {
		return nil, err
	}

	url := u.BaseURL + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+u.APIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llmgw: embed http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(body)
		if len(snippet) > 400 {
			snippet = snippet[:400] + "..."
		}
		return nil, fmt.Errorf("llmgw: embed status %d: %s", resp.StatusCode, snippet)
	}

	var decoded embedResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("llmgw: embed decode: %w", err)
	}
	if len(decoded.Data) == 0 {
		return nil, fmt.Errorf("llmgw: embed: empty data")
	}
	out := make([][]float32, len(decoded.Data))
	for _, d := range decoded.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	for i, v := range out {
		if v == nil {
			return nil, fmt.Errorf("llmgw: embed: missing vector at index %d", i)
		}
	}
	return out, nil
}

// EmbedOne embeds a single text.
func (g *Gateway) EmbedOne(ctx context.Context, text string) ([]float32, error) {
	vecs, err := g.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}
