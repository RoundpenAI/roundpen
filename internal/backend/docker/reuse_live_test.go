package docker_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/backend"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
)

func TestCreateReusesExistingName(t *testing.T) {
	if os.Getenv("ROUNDPEN_TEST_DOCKER_LOCAL") == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_LOCAL=1 to run against the local Docker daemon")
	}
	be, err := dockerbackend.New("", "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer be.Close()

	ctx := context.Background()
	const sandboxID = "test-reuse"
	_ = be.Remove(ctx, sandboxID)
	defer func() { _ = be.Remove(ctx, sandboxID) }()

	ws := t.TempDir()
	first, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: sandboxID,
		Image:     "roundpen-code-agent:local",
		MountDir:  ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := be.Start(ctx, sandboxID); err != nil {
		t.Fatal(err)
	}
	second, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: sandboxID,
		Image:     "roundpen-code-agent:local",
		MountDir:  ws,
	})
	if err != nil {
		t.Fatalf("reuse create: %v", err)
	}
	if first != second {
		t.Fatalf("want reuse id %s, got %s", first, second)
	}
	if err := be.Start(ctx, sandboxID); err != nil {
		t.Fatalf("start already running: %v", err)
	}

	otherWS := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(otherWS, 0o755); err != nil {
		t.Fatal(err)
	}
	replaced, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: sandboxID,
		Image:     "roundpen-code-agent:local",
		MountDir:  otherWS,
	})
	if err != nil {
		t.Fatalf("replace mismatched mount: %v", err)
	}
	if replaced == first {
		t.Fatal("mount mismatch should recreate")
	}
}
