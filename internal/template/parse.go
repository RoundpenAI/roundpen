package template

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var namePartRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-_]{0,127}$`)
var tagPartRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// ParsedRef is a normalized template reference.
type ParsedRef struct {
	Namespace string
	Name      string
	Tag       string
	BuildID   string // set when ref is a bare build UUID
}

// ParseRef interprets an E2B-style template reference.
func ParseRef(raw string) ParsedRef {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedRef{Namespace: DefaultNamespace, Tag: DefaultTag}
	}
	if id, err := uuid.Parse(raw); err == nil {
		return ParsedRef{BuildID: id.String()}
	}

	ns := DefaultNamespace
	name := raw
	tag := DefaultTag

	if i := strings.Index(raw, "/"); i >= 0 {
		ns = strings.ToLower(strings.TrimSpace(raw[:i]))
		name = strings.TrimSpace(raw[i+1:])
	}
	if j := strings.LastIndex(name, ":"); j >= 0 {
		left := name[:j]
		right := strings.TrimSpace(name[j+1:])
		if right != "" {
			if id, err := uuid.Parse(right); err == nil && left == "" {
				return ParsedRef{BuildID: id.String()}
			}
			name = left
			tag = strings.ToLower(right)
		}
	}
	name = strings.ToLower(strings.TrimSpace(name))
	ns = strings.ToLower(strings.TrimSpace(ns))
	if ns == "" {
		ns = DefaultNamespace
	}
	if tag == "" {
		tag = DefaultTag
	}
	return ParsedRef{Namespace: ns, Name: name, Tag: tag}
}

// ValidateName checks E2B template name rules.
func ValidateName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return namePartRe.MatchString(name)
}

// ValidateTag checks template version tag rules (docker-tag compatible).
func ValidateTag(tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" || tag == "default" {
		return false
	}
	return tagPartRe.MatchString(tag)
}

// DisplayName returns namespace/name for API names[] field.
func DisplayName(ns, name string) string {
	if ns == "" || ns == DefaultNamespace {
		return name
	}
	return ns + "/" + name
}
