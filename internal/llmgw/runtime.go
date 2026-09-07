package llmgw

import (
	"context"

	"github.com/RoundpenAI/roundpen/internal/config"
)

// SetPublicURL updates the configured public base URL for setup docs.
func (g *Gateway) SetPublicURL(url string) {
	g.mu.Lock()
	g.publicURL = url
	g.mu.Unlock()
}

// SetEnabled toggles relay forwarding without unmounting HTTP routes.
func (g *Gateway) SetEnabled(v bool) {
	g.mu.Lock()
	g.enabled = v
	g.mu.Unlock()
}

// Enabled reports whether relay forwarding is on.
func (g *Gateway) Enabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.enabled
}

func (g *Gateway) publicBase() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.publicURL
}

// SetDefaultModel sets the fallback model used when a request model is unknown.
func (g *Gateway) SetDefaultModel(model string) {
	g.mu.Lock()
	g.defaultModel = model
	g.mu.Unlock()
}

func (g *Gateway) defaultModelName() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.defaultModel
}

// DefaultModel returns the configured fallback model name.
func (g *Gateway) DefaultModel() string {
	return g.defaultModelName()
}

func (g *Gateway) bodyLogLimit() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.logLimit
}

// SetLogBodyMaxBytes updates request/response body logging limits.
// Values < 0 use the default (64 KiB); 0 disables body logging.
func (g *Gateway) SetLogBodyMaxBytes(n int) {
	limit := n
	if limit < 0 {
		limit = defaultLogBodyMaxBytes
	}
	g.mu.Lock()
	g.logLimit = limit
	g.mu.Unlock()
}

// ApplyConfig hot-applies gateway settings and re-seeds PG vault rows.
func (g *Gateway) ApplyConfig(ctx context.Context, cfg config.LLMGWConfig) error {
	g.SetEnabled(cfg.Enabled)
	g.SetPublicURL(cfg.PublicURL)
	g.SetLogBodyMaxBytes(cfg.LogBodyMaxBytes)
	g.SetDefaultModel(cfg.DefaultModel)

	seed := SeedConfig{Keys: make([]VirtualKey, 0, len(cfg.VirtualKeys))}
	if cfg.OpenAI != nil {
		seed.OpenAI = &UpstreamSeed{
			BaseURL: cfg.OpenAI.BaseURL,
			APIKey:  cfg.OpenAI.APIKey,
			ModelMap: map[string]string{
				EmbeddingModelAlias: cfg.EmbeddingModel,
			},
		}
	}
	if cfg.Anthropic != nil {
		seed.Anthropic = &UpstreamSeed{
			BaseURL: cfg.Anthropic.BaseURL,
			APIKey:  cfg.Anthropic.APIKey,
		}
	}
	for _, vk := range cfg.VirtualKeys {
		seed.Keys = append(seed.Keys, VirtualKey{Key: vk.Key, Name: vk.Name})
	}
	if err := g.SeedFromConfig(seed); err != nil {
		return err
	}
	if cfg.Enabled {
		return g.EnsureInternal(ctx, cfg.EmbeddingModel)
	}
	return nil
}
