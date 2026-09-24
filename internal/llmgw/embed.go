package llmgw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Internal virtual key naming. The key value itself is random per instance
// (see internalkey.go); the legacy hardcoded constant is retired on seed.
const (
	// LegacyInternalVirtualKey was the historical hardcoded internal key. It is
	// deleted from the vault on EnsureInternal so old deployments stop
	// accepting the publicly known value.
	LegacyInternalVirtualKey = "vk-roundpen-internal"

	InternalVirtualName = "roundpen-internal"

	// EmbeddingModelAlias is the client-facing model name for embeddings.
	// Seeded into openai.model_map → real upstream embedding model.
	EmbeddingModelAlias = "roundpen-embed"

	// DefaultEmbeddingModel is the upstream OpenAI-compatible embedding model.
	DefaultEmbeddingModel = "text-embedding-3-small"

	// EmbeddingDimensions matches memory.EmbeddingDims (VECTOR(1024)).
	EmbeddingDimensions = 1024
)

// InternalKey returns the per-instance internal virtual key used by
// control-plane services (memory embedder, automode, sysagent). Agents get
// per-user keys from UserKeyManager instead.
func (g *Gateway) InternalKey() string { return g.internalKey }

// EnsureInternal seeds the Roundpen-internal virtual key used by control-plane
// services. The embedding provider and model are selected through the
// llm.embedding slot (see SetEmbedding) rather than baked into an upstream.
func (g *Gateway) EnsureInternal(ctx context.Context) error {
	now := time.Now().UTC()

	if g.internalKey == "" {
		return fmt.Errorf("seed internal virtual key: no instance key available")
	}
	if err := g.store.UpsertVirtualKey(VirtualKey{
		Key:       g.internalKey,
		Name:      InternalVirtualName,
		Enabled:   true,
		CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("seed internal virtual key: %w", err)
	}
	if err := g.store.DeleteVirtualKey(ctx, LegacyInternalVirtualKey); err != nil {
		return fmt.Errorf("retire legacy internal virtual key: %w", err)
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

// Embed generates embeddings through the provider the llm.embedding slot
// selected. texts must be non-empty.
func (g *Gateway) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("llmgw: embed: empty input")
	}
	u, boundModel, ok := g.embeddingTarget()
	if !ok {
		return nil, fmt.Errorf("llmgw: embed: no embedding provider configured")
	}
	matcher, err := NewModelMatcher(u.ModelMap, u.ModelPatterns)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(boundModel)
	if model == "" {
		model = u.DefaultModel
	}
	if mapped, ok := matcher.Map(model); ok {
		model = mapped
	} else if target, ok := u.ModelMap[EmbeddingModelAlias]; ok && target != "" && model == "" {
		model = target
	}
	if model == "" {
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
	// The shared/streaming clients carry no timeout, so bound the embed call here.
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+u.APIKey)

	client, err := g.clientFor(u.ProxyURL)
	if err != nil {
		return nil, err
	}
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
