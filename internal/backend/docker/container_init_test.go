package docker_test

import (
	"context"
	"os"
	"testing"

	dockerclient "github.com/docker/docker/client"

	"github.com/RoundpenAI/roundpen/internal/backend"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
)

// TestCreateRunsWithInit: sandboxes must run with docker-init, otherwise
// processes orphaned by detached exec sessions (background Bash jobs) are
// never reaped by PID 1.
func TestCreateRunsWithInit(t *testing.T) {
	if os.Getenv("ROUNDPEN_TEST_DOCKER_LOCAL") == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_LOCAL=1 to run against the local Docker daemon")
	}
	be, err := dockerbackend.New("", "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer be.Close()

	ctx := context.Background()
	const sandboxID = "test-init"
	_ = be.Remove(ctx, sandboxID)
	if _, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: sandboxID,
		Image:     "roundpen-code-agent:local",
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = be.Remove(ctx, sandboxID) }()

	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer cli.Close()
	info, err := cli.ContainerInspect(ctx, "roundpen-"+sandboxID)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if info.HostConfig == nil || info.HostConfig.Init == nil || !*info.HostConfig.Init {
		t.Fatal("container must be created with docker-init")
	}
}
