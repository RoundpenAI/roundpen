package secretbox

// SecretMask replaces sensitive values in API responses.
const SecretMask = "●●●●●●●●"

// IsMasked reports whether a submitted value is the response mask.
func IsMasked(v string) bool { return v == SecretMask }

// Mask returns SecretMask when v is non-empty.
func Mask(v string) string {
	if v == "" {
		return ""
	}
	return SecretMask
}

// ResolveValue keeps the previous secret when the client submits empty or masked.
func ResolveValue(submitted, previous string) string {
	if submitted == "" || IsMasked(submitted) {
		return previous
	}
	return submitted
}
