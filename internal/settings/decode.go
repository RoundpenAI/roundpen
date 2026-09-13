package settings

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/config"
)

// DecodeAppSettings unmarshals stored JSON on top of fallback so newly added
// keys (e.g. llmgw) keep env defaults when older rows omit them.
func DecodeAppSettings(raw []byte, fallback AppSettings) (AppSettings, error) {
	out := fallback
	if len(raw) == 0 {
		return normalizeLegacy(out), nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return AppSettings{}, fmt.Errorf("decode settings: %w", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return normalizeLegacy(out), nil
	}
	if _, ok := keys["llmgwEnabled"]; !ok {
		out.LlmgwEnabled = fallback.LlmgwEnabled
		out.LlmgwPublicURL = fallback.LlmgwPublicURL
		out.LlmgwLogBodyMaxBytes = fallback.LlmgwLogBodyMaxBytes
		out.LlmgwEmbeddingModel = fallback.LlmgwEmbeddingModel
		out.LlmgwDefaultModel = fallback.LlmgwDefaultModel
		out.LlmgwOpenaiBaseURL = fallback.LlmgwOpenaiBaseURL
		out.LlmgwOpenaiAPIKey = fallback.LlmgwOpenaiAPIKey
		out.LlmgwAnthropicBaseURL = fallback.LlmgwAnthropicBaseURL
		out.LlmgwAnthropicAPIKey = fallback.LlmgwAnthropicAPIKey
		out.LlmgwVirtualKeys = fallback.LlmgwVirtualKeys
	}
	if _, ok := keys["llmgwDefaultModel"]; !ok {
		out.LlmgwDefaultModel = fallback.LlmgwDefaultModel
	}
	if _, ok := keys["cdpProvider"]; !ok {
		out.CDPProvider = fallback.CDPProvider
		out.CDPEndpoint = fallback.CDPEndpoint
		out.CDPToken = fallback.CDPToken
		out.CDPPort = fallback.CDPPort
	}
	return normalizeLegacy(out), nil
}

// normalizeLegacy maps pre-removal values onto their current equivalents so old
// settings rows keep working. Removing kaniko from the validation set would
// otherwise fail the row and reset settings from env.
func normalizeLegacy(s AppSettings) AppSettings {
	if strings.EqualFold(strings.TrimSpace(s.TemplateBuilder), "kaniko") {
		s.TemplateBuilder = "docker"
	}
	// Pre-browserless rows persist 9222, the old managed-container default. The
	// container now serves browserless on config.DefaultCDPPort (3000), so a
	// stale 9222 makes the hub dial a closed port. Migrate it for the providers
	// that own the container; remote/cloud rows keep whatever they pinned.
	switch strings.ToLower(strings.TrimSpace(s.CDPProvider)) {
	case "", config.CDPProviderAuto, config.CDPProviderDocker:
		if s.CDPPort == 9222 {
			s.CDPPort = config.DefaultCDPPort
		}
	}
	return s
}
