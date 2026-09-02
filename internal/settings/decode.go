package settings

import (
	"encoding/json"
	"fmt"
)

// DecodeAppSettings unmarshals stored JSON on top of fallback so newly added
// keys (e.g. llmgw) keep env defaults when older rows omit them.
func DecodeAppSettings(raw []byte, fallback AppSettings) (AppSettings, error) {
	out := fallback
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return AppSettings{}, fmt.Errorf("decode settings: %w", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return out, nil
	}
	if _, ok := keys["llmgwEnabled"]; !ok {
		out.LlmgwEnabled = fallback.LlmgwEnabled
		out.LlmgwPublicURL = fallback.LlmgwPublicURL
		out.LlmgwLogBodyMaxBytes = fallback.LlmgwLogBodyMaxBytes
		out.LlmgwEmbeddingModel = fallback.LlmgwEmbeddingModel
		out.LlmgwOpenaiBaseURL = fallback.LlmgwOpenaiBaseURL
		out.LlmgwOpenaiAPIKey = fallback.LlmgwOpenaiAPIKey
		out.LlmgwAnthropicBaseURL = fallback.LlmgwAnthropicBaseURL
		out.LlmgwAnthropicAPIKey = fallback.LlmgwAnthropicAPIKey
		out.LlmgwVirtualKeys = fallback.LlmgwVirtualKeys
	}
	if _, ok := keys["kanikoExecutor"]; !ok {
		out.KanikoExecutor = fallback.KanikoExecutor
	}
	if _, ok := keys["kanikoRegistryMirrors"]; !ok {
		out.KanikoRegistryMirrors = fallback.KanikoRegistryMirrors
	}
	if _, ok := keys["cdpProvider"]; !ok {
		out.CDPProvider = fallback.CDPProvider
		out.CDPEndpoint = fallback.CDPEndpoint
		out.CDPToken = fallback.CDPToken
		out.CDPPort = fallback.CDPPort
	}
	return out, nil
}
