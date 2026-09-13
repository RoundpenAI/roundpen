package config

import "strings"

// ResolveTemplateBuilder returns docker, ci, or empty when builds are disabled.
func (c *Config) ResolveTemplateBuilder() string {
	mode := strings.ToLower(strings.TrimSpace(c.TemplateBuilder))
	if mode == "disabled" {
		mode = ""
	}
	switch mode {
	case "docker", "ci":
		return mode
	case "kaniko":
		// Legacy alias: the kaniko executor was replaced by the docker builder.
		return "docker"
	case "auto":
		if strings.EqualFold(c.Backend, "docker") {
			return "docker"
		}
		return ""
	case "":
		return ""
	default:
		return ""
	}
}
