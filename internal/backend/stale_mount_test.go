package backend_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

func TestIsStaleWorkspaceMount(t *testing.T) {
	if backend.IsStaleWorkspaceMount(nil) {
		t.Fatal("nil")
	}
	if !backend.IsStaleWorkspaceMount(backend.ErrStaleWorkspaceMount) {
		t.Fatal("sentinel")
	}
	wrapped := fmt.Errorf("exec create: %w", backend.ErrStaleWorkspaceMount)
	if !backend.IsStaleWorkspaceMount(wrapped) {
		t.Fatal("wrapped sentinel")
	}
	oci := errors.New("Error response from daemon: OCI runtime exec failed: exec failed: unable to start container process: current working directory is outside of container mount namespace root -- possible container breakout detected: unknown")
	if !backend.IsStaleWorkspaceMount(oci) {
		t.Fatal("oci message")
	}
	if backend.IsStaleWorkspaceMount(errors.New("no such container")) {
		t.Fatal("unrelated")
	}
}

func TestLooksLikeStaleWorkspaceMountFromOutput(t *testing.T) {
	out := []byte("OCI runtime exec failed: ... outside of container mount namespace root -- possible container breakout detected\r\n")
	if !backend.LooksLikeStaleWorkspaceMount(nil, 128, out, nil) {
		t.Fatal("exit 128 + phrase")
	}
	if backend.LooksLikeStaleWorkspaceMount(nil, 0, out, nil) {
		t.Fatal("exit 0 should not count phrase alone")
	}
	if backend.LooksLikeStaleWorkspaceMount(nil, 1, []byte("permission denied"), nil) {
		t.Fatal("unrelated failure")
	}
}
