package config

import (
	"os"
	"strings"
)

// WebToolsConfig configures the optional web tools (WebSearch via Tavily).
// endpoint 与 key 都为空时不注册 WebSearch。
type WebToolsConfig struct {
	SearchEndpoint string // ROUNDPEN_WEB_SEARCH_ENDPOINT；空 = 默认 https://api.tavily.com
	SearchAPIKey   string // ROUNDPEN_WEB_SEARCH_API_KEY
}

func loadWebTools() WebToolsConfig {
	return WebToolsConfig{
		SearchEndpoint: strings.TrimSpace(os.Getenv("ROUNDPEN_WEB_SEARCH_ENDPOINT")),
		SearchAPIKey:   strings.TrimSpace(os.Getenv("ROUNDPEN_WEB_SEARCH_API_KEY")),
	}
}
