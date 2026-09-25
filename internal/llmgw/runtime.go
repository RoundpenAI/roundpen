package llmgw

import (
	"context"
	"sort"

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

// SetUpstreams replaces the provider set the relay serves. The control plane
// projects it from the setting items on boot and after every catalog reload.
func (g *Gateway) SetUpstreams(list []Upstream) {
	next := make(map[string]Upstream, len(list))
	for _, u := range list {
		next[u.Provider] = u
	}
	g.mu.Lock()
	g.upstreams = next
	g.mu.Unlock()
}

// Upstream returns a provider by item id.
func (g *Gateway) Upstream(id string) (Upstream, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	u, ok := g.upstreams[id]
	if !ok || !u.Enabled {
		return Upstream{}, false
	}
	return u, true
}

// Upstreams returns every configured provider, ordered by id.
func (g *Gateway) Upstreams() []Upstream {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Upstream, 0, len(g.upstreams))
	for _, u := range g.upstreams {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// SetEmbedding selects the provider and model behind Embed ("" disables it).
func (g *Gateway) SetEmbedding(provider, model string) {
	g.mu.Lock()
	g.embedProvider = provider
	g.embedModel = model
	g.mu.Unlock()
}

func (g *Gateway) embeddingTarget() (Upstream, string, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	u, ok := g.upstreams[g.embedProvider]
	if !ok || !u.Enabled || g.embedProvider == "" {
		return Upstream{}, "", false
	}
	return u, g.embedModel, true
}

func (g *Gateway) bodyLogLimit() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.logLimit
}

// SetLogBodyMaxBytes updates request/response body logging limits.
// Values <= 0 disable body logging. Positive values cap stored bytes.
func (g *Gateway) SetLogBodyMaxBytes(n int) {
	limit := n
	if limit < 0 {
		limit = 0
	}
	g.mu.Lock()
	g.logLimit = limit
	g.mu.Unlock()
}

// ApplyConfig hot-applies the gateway switches and virtual keys. Providers
// themselves arrive through SetUpstreams (they live as setting items).
func (g *Gateway) ApplyConfig(ctx context.Context, cfg config.LLMGWConfig) error {
	g.SetEnabled(cfg.Enabled)
	g.SetPublicURL(cfg.PublicURL)
	g.SetLogBodyMaxBytes(cfg.LogBodyMaxBytes)

	for _, vk := range cfg.VirtualKeys {
		name := vk.Name
		if name == "" {
			name = vk.Key
		}
		if err := g.store.UpsertVirtualKey(VirtualKey{Key: vk.Key, Name: name, Enabled: true}); err != nil {
			return err
		}
	}
	if cfg.Enabled {
		return g.EnsureInternal(ctx)
	}
	return nil
}
