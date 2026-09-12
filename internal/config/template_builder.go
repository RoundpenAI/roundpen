package config

import "strings"

// ResolveTemplateBuilder returns docker, kaniko, or empty when builds are disabled.
func (c *Config) ResolveTemplateBuilder() string {
	mode := strings.ToLower(strings.TrimSpace(c.TemplateBuilder))
	if mode == "disabled" {
		mode = ""
	}
	switch mode {
	case "docker", "kaniko", "ci":
		return mode
	case "auto":
		if strings.EqualFold(c.Backend, "docker") {
			return "docker"
		}
		// Explicit auto + destination may select kaniko; AttachBuilder soft-disables if executor missing.
		if c.KanikoDestination != "" {
			return "kaniko"
		}
		return ""
	case "":
		return ""
	default:
		return ""
	}
}
