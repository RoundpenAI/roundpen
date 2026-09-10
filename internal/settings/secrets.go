package settings

import (
	"strings"

	"github.com/RoundpenAI/roundpen/internal/config"
)

// SecretMask is returned for sensitive settings fields in GET responses.
const SecretMask = "●●●●●●●●"

// IsSecretMask reports whether a submitted secret should be treated as unchanged.
func IsSecretMask(v string) bool {
	return v == SecretMask
}

// MaskSecret returns SecretMask when v is non-empty.
func MaskSecret(v string) string {
	if v == "" {
		return ""
	}
	return SecretMask
}

// ResolveSecret keeps the previous secret when the client submits empty or masked.
func ResolveSecret(submitted, previous string) string {
	if submitted == "" || IsSecretMask(submitted) {
		return previous
	}
	return submitted
}

// MaskVirtualKeysSetting masks key material in "vk-dev:dev,vk-prod" form.
func MaskVirtualKeysSetting(raw string) string {
	keys, err := config.ParseVirtualKeys(raw)
	if err != nil || len(keys) == 0 {
		return MaskSecret(raw)
	}
	parts := make([]string, 0, len(keys))
	for _, vk := range keys {
		masked := maskVirtualKeyMaterial(vk.Key)
		if vk.Name != "" && vk.Name != vk.Key {
			parts = append(parts, masked+":"+vk.Name)
		} else {
			parts = append(parts, masked)
		}
	}
	return strings.Join(parts, ",")
}

// ResolveVirtualKeysSetting keeps previous keys when the client sent a masked list.
func ResolveVirtualKeysSetting(submitted, previous string) string {
	if submitted == "" || IsSecretMask(submitted) || strings.Contains(submitted, "****") {
		return previous
	}
	return submitted
}

func maskVirtualKeyMaterial(key string) string {
	if key == "" {
		return ""
	}
	if !strings.HasPrefix(key, "vk-") || len(key) < 8 {
		return "vk-****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
