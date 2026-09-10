package runtime

import "strings"

const (
	EngineQEMU   = "qemu"
	EngineDocker = "docker"
	EngineKern   = "kern"
)

// NormalizeEngine returns qemu, docker, or kern (empty if unset/invalid).
func NormalizeEngine(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case EngineQEMU, EngineDocker, EngineKern:
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return ""
	}
}

// TemplateForEngine is the default slot template for an agent engine.
func TemplateForEngine(engine string) string {
	switch NormalizeEngine(engine) {
	case EngineDocker:
		return "code-agent"
	case EngineKern:
		return "host"
	default:
		return "agent-claude"
	}
}

// EngineOfImage infers which engine a sandbox image belongs to.
func EngineOfImage(image string) string {
	img := strings.ToLower(strings.TrimSpace(image))
	if strings.Contains(img, ".qcow2") {
		return EngineQEMU
	}
	if img == "host" || img == "" {
		return EngineKern
	}
	return EngineDocker
}
