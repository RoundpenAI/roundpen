package docker_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
)

func TestRemoteSSHPing(t *testing.T) {
	host := os.Getenv("ROUNDPEN_TEST_DOCKER_HOST")
	if host == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_HOST=ssh://user@host to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	be, err := dockerbackend.New(host, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer be.Close()
	if err := be.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestRemoteSSHCreateExec(t *testing.T) {
	host := os.Getenv("ROUNDPEN_TEST_DOCKER_HOST")
	mount := os.Getenv("ROUNDPEN_TEST_REMOTE_MOUNT")
	if host == "" || mount == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_HOST and ROUNDPEN_TEST_REMOTE_MOUNT")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	be, err := dockerbackend.New(host, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer be.Close()

	id := "ssh-probe-" + time.Now().Format("150405")
	defer func() { _ = be.Remove(context.Background(), id) }()

	if _, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: id,
		Image:     "alpine:3.20",
		MountDir:  mount,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := be.Start(ctx, id); err != nil {
		t.Fatalf("start: %v", err)
	}
	res, err := be.Exec(ctx, id, backend.ExecOpts{
		Cmd:     []string{"sh", "-c", "echo ok > /workspace/probe.txt && cat /workspace/probe.txt"},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(string(res.Stdout), "ok") {
		t.Fatalf("stdout=%q", res.Stdout)
	}
}
