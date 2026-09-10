package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LLMGWConfig holds LLM gateway bootstrap settings (seeded into PostgreSQL).
type LLMGWConfig struct {
	Enabled         bool
	PublicURL       string
	LogBodyMaxBytes int    // 0 = disable body logging (default); >0 caps stored bytes
	EmbeddingModel  string // upstream model for roundpen-embed alias
	OpenAI          *LLMGWUpstream
	Anthropic       *LLMGWUpstream
	VirtualKeys     []LLMGWVirtualKey
}

// LLMGWUpstream is env/bootstrap input for a provider vault row.
type LLMGWUpstream struct {
	BaseURL string
	APIKey  string
}

// LLMGWVirtualKey is a client-facing credential seed.
type LLMGWVirtualKey struct {
	Key  string
	Name string
}

func loadLLMGW() (LLMGWConfig, error) {
	cfg := LLMGWConfig{
		PublicURL:       strings.TrimSpace(os.Getenv("ROUNDPEN_LLMGW_PUBLIC_URL")),
		LogBodyMaxBytes: 0,
		EmbeddingModel:  strings.TrimSpace(os.Getenv("ROUNDPEN_LLMGW_EMBEDDING_MODEL")),
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "text-embedding-3-small"
	}

	if v := os.Getenv("ROUNDPEN_LLMGW_LOG_BODY_MAX_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return cfg, fmt.Errorf("ROUNDPEN_LLMGW_LOG_BODY_MAX_BYTES: must be >= 0")
		}
		cfg.LogBodyMaxBytes = n
	}

	openaiBase := strings.TrimSpace(os.Getenv("ROUNDPEN_LLMGW_OPENAI_BASE_URL"))
	openaiKey := strings.TrimSpace(os.Getenv("ROUNDPEN_LLMGW_OPENAI_API_KEY"))
	if openaiBase != "" || openaiKey != "" {
		if openaiBase == "" || openaiKey == "" {
			return cfg, fmt.Errorf("ROUNDPEN_LLMGW_OPENAI_BASE_URL and ROUNDPEN_LLMGW_OPENAI_API_KEY must both be set")
		}
		cfg.OpenAI = &LLMGWUpstream{BaseURL: openaiBase, APIKey: openaiKey}
	}

	antBase := strings.TrimSpace(os.Getenv("ROUNDPEN_LLMGW_ANTHROPIC_BASE_URL"))
	antKey := strings.TrimSpace(os.Getenv("ROUNDPEN_LLMGW_ANTHROPIC_API_KEY"))
	if antBase != "" || antKey != "" {
		if antBase == "" || antKey == "" {
			return cfg, fmt.Errorf("ROUNDPEN_LLMGW_ANTHROPIC_BASE_URL and ROUNDPEN_LLMGW_ANTHROPIC_API_KEY must both be set")
		}
		cfg.Anthropic = &LLMGWUpstream{BaseURL: antBase, APIKey: antKey}
	}

	keys, err := ParseVirtualKeys(os.Getenv("ROUNDPEN_LLMGW_VIRTUAL_KEYS"))
	if err != nil {
		return cfg, err
	}
	cfg.VirtualKeys = keys

	hasSeed := cfg.OpenAI != nil || cfg.Anthropic != nil || len(cfg.VirtualKeys) > 0
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ROUNDPEN_LLMGW_ENABLED"))) {
	case "1", "true", "yes", "on":
		cfg.Enabled = true
	case "0", "false", "no", "off":
		cfg.Enabled = false
	default:
		cfg.Enabled = hasSeed
	}

	return cfg, nil
}

// ParseVirtualKeys parses "vk-dev:dev,vk-prod" (name defaults to key when omitted).
func ParseVirtualKeys(raw string) ([]LLMGWVirtualKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]LLMGWVirtualKey, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key, name, ok := strings.Cut(p, ":")
		key = strings.TrimSpace(key)
		name = strings.TrimSpace(name)
		if key == "" {
			return nil, fmt.Errorf("ROUNDPEN_LLMGW_VIRTUAL_KEYS: empty key in %q", p)
		}
		if !ok || name == "" {
			name = key
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("ROUNDPEN_LLMGW_VIRTUAL_KEYS: duplicate key %q", key)
		}
		seen[key] = struct{}{}
		out = append(out, LLMGWVirtualKey{Key: key, Name: name})
	}
	return out, nil
}
