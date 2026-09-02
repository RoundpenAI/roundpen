package settings

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
