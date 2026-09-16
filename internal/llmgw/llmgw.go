// Package llmgw is the LLM gateway: virtual-key vault, upstream proxy, and PG request log.
// Protocol model follows model-relay (passthrough Anthropic / OpenAI dialects).
package llmgw

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"

	defaultLogBodyMaxBytes = 0
)

// Options configures the gateway.
type Options struct {
	LogBodyMaxBytes int
	PublicURL       string
	Logger          *slog.Logger
}

// Gateway serves LLM relay and admin HTTP endpoints backed by PostgreSQL.
type Gateway struct {
	store      *Store
	logger     *slog.Logger
	httpClient *http.Client

	clientMu     sync.Mutex
	proxyClients map[string]*http.Client // keyed by proxy URL

	mu           sync.RWMutex
	enabled      bool
	logLimit     int
	publicURL    string
	defaultModel string
}

// New builds a Gateway. Call SeedFromConfig after construction to bootstrap from env.
func New(db *storage.DB, opts Options) *Gateway {
	limit := opts.LogBodyMaxBytes
	if limit < 0 {
		limit = 0
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{
		store:        NewStore(db),
		enabled:      true,
		logLimit:     limit,
		publicURL:    opts.PublicURL,
		logger:       logger,
		proxyClients: map[string]*http.Client{},
		httpClient: &http.Client{
			Timeout: 0, // streaming
		},
	}
}

// clientFor returns an HTTP client honoring the upstream's egress proxy,
// cached per proxy URL. An empty proxy URL means the shared direct client.
func (g *Gateway) clientFor(proxyURL string) (*http.Client, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return g.httpClient, nil
	}
	g.clientMu.Lock()
	defer g.clientMu.Unlock()
	if c, ok := g.proxyClients[proxyURL]; ok {
		return c, nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid upstream proxy URL %q", proxyURL)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyURL(u)
	c := &http.Client{Transport: tr, Timeout: 0} // streaming
	g.proxyClients[proxyURL] = c
	return c, nil
}

// Store returns the underlying PG store (for tests / seed).
func (g *Gateway) Store() *Store { return g.store }

// SeedConfig holds optional bootstrap values written into PG on startup.
type SeedConfig struct {
	OpenAI    *UpstreamSeed
	Anthropic *UpstreamSeed
	Keys      []VirtualKey
}

// UpstreamSeed is env/bootstrap input for an upstream provider.
type UpstreamSeed struct {
	BaseURL       string
	APIKey        string
	ProxyURL      string
	ModelMap      map[string]string
	ModelPatterns []ModelPattern
}

// SeedFromConfig upserts upstreams and virtual keys from process config into PG.
func (g *Gateway) SeedFromConfig(cfg SeedConfig) error {
	now := time.Now().UTC()
	for provider, seed := range map[string]*UpstreamSeed{
		ProviderAnthropic: cfg.Anthropic,
		ProviderOpenAI:    cfg.OpenAI,
	} {
		if seed == nil || seed.BaseURL == "" || seed.APIKey == "" {
			continue
		}
		u := Upstream{
			Provider:      provider,
			BaseURL:       trimRightSlash(seed.BaseURL),
			APIKey:        seed.APIKey,
			ProxyURL:      strings.TrimSpace(seed.ProxyURL),
			ModelMap:      seed.ModelMap,
			ModelPatterns: seed.ModelPatterns,
			Enabled:       true,
			UpdatedAt:     now,
		}
		if u.ModelMap == nil {
			u.ModelMap = map[string]string{}
		}
		if err := g.store.UpsertUpstream(u); err != nil {
			return err
		}
	}
	for _, vk := range cfg.Keys {
		if vk.Key == "" {
			continue
		}
		if vk.Name == "" {
			vk.Name = vk.Key
		}
		vk.Enabled = true
		if vk.CreatedAt.IsZero() {
			vk.CreatedAt = now
		}
		if err := g.store.UpsertVirtualKey(vk); err != nil {
			return err
		}
	}
	return nil
}

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
