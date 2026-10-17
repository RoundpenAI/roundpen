package llmgw

import (
	"strings"
	"unicode"
)

// joinUpstreamURL appends a client path to an upstream base without doubling
// a version prefix. OpenRouter-style bases already end in /v1, and Zhipu's
// GLM base ends in /v4; OpenAI SDKs still send /v1/chat/completions.
func joinUpstreamURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if versionedBase(base) && (path == "/v1" || strings.HasPrefix(path, "/v1/")) {
		path = strings.TrimPrefix(path, "/v1")
		if path == "" {
			path = "/"
		}
	}
	return base + path
}

// versionedBase reports whether base already ends in an API generation such
// as /v1 or /v4.
func versionedBase(base string) bool {
	i := strings.LastIndex(base, "/v")
	if i < 0 || i+2 >= len(base) {
		return false
	}
	rest := base[i+2:]
	if rest == "" {
		return false
	}
	for _, r := range rest {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
