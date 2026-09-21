package config

import (
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
