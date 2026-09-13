package docker_test

import (
	"context"
	"os"
	"testing"

	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
)

// TestRefreshImageDigestStable: pulling an already-current image reports no
// change on the second call.
func TestRefreshImageDigestStable(t *testing.T) {
	if os.Getenv("ROUNDPEN_TEST_DOCKER_LOCAL") == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_LOCAL=1 to run against the local Docker daemon")
	}
	be, err := dockerbackend.New("", "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer be.Close()

	ctx := context.Background()
	const ref = "ghcr.io/roundpenai/code-agent:latest"
	if _, _, err := be.RefreshImage(ctx, ref); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	changed, digest, err := be.RefreshImage(ctx, ref)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if changed {
		t.Fatal("second refresh reported a change")
	}
	if digest == "" {
		t.Fatal("empty digest")
	}
}
