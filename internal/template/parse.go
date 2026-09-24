package template

import "strings"

// ParsedRef is a normalized template reference.
type ParsedRef struct {
	Namespace string
	Name      string
}

// ParseRef interprets a template reference (name, namespace/name, or
// namespace/name:tag; tags are parsed but the catalog holds one artifact per
// name, so the tag does not affect resolution).
func ParseRef(raw string) ParsedRef {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedRef{Namespace: DefaultNamespace}
	}

	ns := DefaultNamespace
	name := raw
	if i := strings.Index(raw, "/"); i >= 0 {
		ns = strings.ToLower(strings.TrimSpace(raw[:i]))
		name = strings.TrimSpace(raw[i+1:])
	}
	if j := strings.LastIndex(name, ":"); j >= 0 {
		left := name[:j]
		right := strings.TrimSpace(name[j+1:])
		if right != "" {
			name = left
		}
	}
	name = strings.ToLower(strings.TrimSpace(name))
	ns = strings.ToLower(strings.TrimSpace(ns))
	if ns == "" {
		ns = DefaultNamespace
	}
	return ParsedRef{Namespace: ns, Name: name}
}

// DisplayName returns namespace/name for API names[] field.
func DisplayName(ns, name string) string {
	if ns == "" || ns == DefaultNamespace {
		return name
	}
	return ns + "/" + name
}
