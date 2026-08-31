package llmgw

import (
	"context"

	"github.com/RoundpenAI/roundpen/internal/config"
)

// SetPublicURL updates the configured public base URL for setup docs.
func (g *Gateway) SetPublicURL(url string) {
	g.publicURL = url
}

// SetLogBodyMaxBytes updates request/response body logging limits.
// Values < 0 use the default (64 KiB); 0 disables body logging.
func (g *Gateway) SetLogBodyMaxBytes(n int) {
	limit := n
	if limit < 0 {
		limit = defaultLogBodyMaxBytes
	}
	g.logLimit = limit
}

// ApplyConfig hot-applies gateway settings and re-seeds PG vault rows.
func (g *Gateway) ApplyConfig(ctx context.Context, cfg config.LLMGWConfig) error {
	g.SetPublicURL(cfg.PublicURL)
	g.SetLogBodyMaxBytes(cfg.LogBodyMaxBytes)

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
