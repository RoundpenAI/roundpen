package config

import (
	"fmt"
	"strings"
)

// FormatVirtualKeys serializes virtual keys for settings UI ("vk-dev:dev,vk-prod").
func FormatVirtualKeys(keys []LLMGWVirtualKey) string {
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, 0, len(keys))
	for _, vk := range keys {
		key := strings.TrimSpace(vk.Key)
		if key == "" {
			continue
		}
		name := strings.TrimSpace(vk.Name)
		if name != "" && name != key {
			parts = append(parts, key+":"+name)
		} else {
			parts = append(parts, key)
		}
	}
	return strings.Join(parts, ",")
}

// ApplyLLMGWSettings writes admin settings into LLMGWConfig.
func ApplyLLMGWSettings(dst *LLMGWConfig, enabled bool, publicURL, embeddingModel, defaultModel string, logBodyMaxBytes int, openaiBase, openaiKey, anthropicBase, anthropicKey, virtualKeysRaw string) error {
	dst.Enabled = enabled
	dst.PublicURL = strings.TrimSpace(publicURL)
	dst.EmbeddingModel = strings.TrimSpace(embeddingModel)
	if dst.EmbeddingModel == "" {
		dst.EmbeddingModel = "text-embedding-3-small"
	}
	dst.DefaultModel = strings.TrimSpace(defaultModel)
	dst.LogBodyMaxBytes = logBodyMaxBytes

	openaiBase = strings.TrimSpace(openaiBase)
	openaiKey = strings.TrimSpace(openaiKey)
	if openaiBase != "" || openaiKey != "" {
		if openaiBase == "" || openaiKey == "" {
			return fmt.Errorf("llmgw openai base URL and API key must both be set")
		}
		dst.OpenAI = &LLMGWUpstream{BaseURL: openaiBase, APIKey: openaiKey}
	} else {
		dst.OpenAI = nil
	}

	anthropicBase = strings.TrimSpace(anthropicBase)
	anthropicKey = strings.TrimSpace(anthropicKey)
	if anthropicBase != "" || anthropicKey != "" {
		if anthropicBase == "" || anthropicKey == "" {
			return fmt.Errorf("llmgw anthropic base URL and API key must both be set")
		}
		dst.Anthropic = &LLMGWUpstream{BaseURL: anthropicBase, APIKey: anthropicKey}
	} else {
		dst.Anthropic = nil
	}

	keys, err := ParseVirtualKeys(virtualKeysRaw)
	if err != nil {
		return err
	}
	dst.VirtualKeys = keys
	return nil
}
