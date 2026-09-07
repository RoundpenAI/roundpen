package config

import "strings"

// ResolveTemplateBuilder returns docker, kaniko, or empty when builds are disabled.
func (c *Config) ResolveTemplateBuilder() string {
	mode := strings.ToLower(strings.TrimSpace(c.TemplateBuilder))
	switch mode {
	case "docker", "kaniko":
		return mode
	case "auto", "":
		if strings.EqualFold(c.Backend, "docker") {
			return "docker"
		}
		if strings.EqualFold(c.Backend, "qemu") && c.KanikoDestination != "" {
			return "kaniko"
		}
		if c.KanikoDestination != "" {
			return "kaniko"
		}
		return ""
	default:
		return ""
	}
}
