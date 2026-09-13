package docker_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
)

// TestExecAbortsOnContextCancel: stopping the caller must abort a running
// exec promptly instead of waiting for the command to finish.
func TestExecAbortsOnContextCancel(t *testing.T) {
	if os.Getenv("ROUNDPEN_TEST_DOCKER_LOCAL") == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_LOCAL=1 to run against the local Docker daemon")
	}
	be, err := dockerbackend.New("", "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer be.Close()

	ctx := context.Background()
	const sandboxID = "test-exec-cancel"
	if _, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: sandboxID,
		Image:     "roundpen-code-agent:local",
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = be.Remove(ctx, sandboxID) }()
	if err := be.Start(ctx, sandboxID); err != nil {
		t.Fatal(err)
	}

	execCtx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(time.Second)
		cancel()
	}()
	start := time.Now()
	if _, err := be.Exec(execCtx, sandboxID, backend.ExecOpts{Cmd: []string{"sleep", "30"}}); err == nil {
		t.Fatal("expected a cancellation error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ctx cancel did not abort exec: took %s", elapsed.Round(time.Second))
	}
}
