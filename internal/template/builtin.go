package template

// BuiltinNames are seeded system templates that cannot be deleted.
// Metadata and resources may be edited; image builds can be triggered by admins.
var BuiltinNames = map[string]struct{}{
	"host":       {},
	"base":       {},
	"python":     {},
	"node":       {},
	"code-agent": {},
}

// IsBuiltin reports whether a template row is a seeded built-in.
func IsBuiltin(namespace, name, createdBy string) bool {
	if namespace != DefaultNamespace {
		return false
	}
	if createdBy == "system" {
		_, ok := BuiltinNames[name]
		return ok
	}
	return false
}
