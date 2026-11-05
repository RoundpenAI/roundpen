package backend

import (
	"errors"
	"strings"
)

// ErrStaleWorkspaceMount means the container's /workspace bind still points at a
// deleted host inode (path was removed and recreated). Exec with WorkingDir
// /workspace then fails with runc's "outside of container mount namespace"
// check. The only recovery is recreating the container so Docker rebinds.
var ErrStaleWorkspaceMount = errors.New("stale workspace bind mount")

// IsStaleWorkspaceMount reports whether err (or nested errors) is a dead
// /workspace bind after the host directory was replaced.
func IsStaleWorkspaceMount(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrStaleWorkspaceMount) {
		return true
	}
	return staleMountMessage(err.Error())
}

// LooksLikeStaleWorkspaceMount checks docker exec create/attach errors and
// process output for the runc cwd-outside-root signature.
func LooksLikeStaleWorkspaceMount(err error, exitCode int, stdout, stderr []byte) bool {
	if IsStaleWorkspaceMount(err) {
		return true
	}
	var b strings.Builder
	if err != nil {
		b.WriteString(err.Error())
		b.WriteByte('\n')
	}
	b.Write(stdout)
	b.WriteByte('\n')
	b.Write(stderr)
	if !staleMountMessage(b.String()) {
		return false
	}
	// runc typically exits 126/128; also accept when the phrase alone is clear.
	if exitCode == 0 && err == nil {
		return false
	}
	return true
}

func staleMountMessage(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "outside of container mount namespace") ||
		strings.Contains(msg, "possible container breakout detected")
}
