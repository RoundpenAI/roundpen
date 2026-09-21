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

	"github.com/RoundpenAI/roundpen/internal/secretbox"
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
	// InternalKeyFile persists the per-instance internal virtual key (0600).
	// Empty means an ephemeral random key (dev/test).
	InternalKeyFile string
	// SecretBox seals upstream API keys at rest. Nil stores them plaintext
	// (tests, single-user dev).
	SecretBox *secretbox.Box
}

// Gateway serves LLM relay and admin HTTP endpoints backed by PostgreSQL.
type Gateway struct {
	store       *Store
	logger      *slog.Logger
	httpClient  *http.Client
	internalKey string

	clientMu     sync.Mutex
	proxyClients map[string]*http.Client // keyed by proxy URL

	mu            sync.RWMutex
	enabled       bool
	logLimit      int
	publicURL     string
	upstreams     map[string]Upstream
	embedProvider string
	embedModel    string
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
		store:        NewStore(db, opts.SecretBox),
		enabled:      true,
		logLimit:     limit,
		publicURL:    opts.PublicURL,
		logger:       logger,
		internalKey:  loadOrGenerateInternalKey(opts.InternalKeyFile, logger),
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

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
