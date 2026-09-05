package llmgw

import "strings"

// joinUpstreamURL appends a client path to an upstream base without doubling
// a trailing /v1 (OpenRouter and many OpenAI-compatible hosts already include it).
func joinUpstreamURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasSuffix(base, "/v1") && (path == "/v1" || strings.HasPrefix(path, "/v1/")) {
		path = strings.TrimPrefix(path, "/v1")
		if path == "" {
			path = "/"
		}
	}
	return base + path
}
