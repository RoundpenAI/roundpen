package runtime

import "strings"

const (
	EngineQEMU   = "qemu"
	EngineDocker = "docker"
)

// NormalizeEngine returns qemu or docker (empty if unset/invalid).
func NormalizeEngine(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case EngineQEMU, EngineDocker:
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return ""
	}
}

// EngineOfImage infers which engine a sandbox image belongs to.
func EngineOfImage(image string) string {
	img := strings.ToLower(strings.TrimSpace(image))
	if strings.Contains(img, ".qcow2") {
		return EngineQEMU
	}
	return EngineDocker
}
