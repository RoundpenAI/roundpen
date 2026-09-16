package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/backend"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

const bgLiveSandboxID = "bglive"

type liveAgentSlots struct{}

func (liveAgentSlots) EnsureAgent(_ context.Context, _ string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{ID: bgLiveSandboxID, Name: "agent-live", Status: sandbox.StatusRunning}, nil
}

// liveExec forwards sandbox exec requests to the backend, the same conversion
// sandbox.Service.Exec performs in production.
type liveExec struct{ b *dockerbackend.Backend }

func (l liveExec) Exec(ctx context.Context, id string, req sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	res, err := l.b.Exec(ctx, id, backend.ExecOpts{
		Cmd:     req.Cmd,
		WorkDir: req.WorkDir,
		Env:     req.Env,
		Timeout: req.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &sandbox.ExecResult{ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

func (l liveExec) WorkspaceHostPath(context.Context, string) (string, error) { return "", nil }

// Live check of the background Bash tools against a real sandbox container.
// Needs a local Docker daemon and the code-agent image:
//
//	docker build -t roundpen-code-agent:local images/code-agent
//	ROUNDPEN_TEST_BG_LIVE=1 go test ./internal/acp/sysagent/tools -run TestLiveBackgroundBash -v
func TestLiveBackgroundBash(t *testing.T) {
	if os.Getenv("ROUNDPEN_TEST_BG_LIVE") == "" {
		t.Skip("set ROUNDPEN_TEST_BG_LIVE=1 to run")
	}
	if _, err := os.Stat("/var/run/docker.sock"); err != nil && os.Getenv("DOCKER_HOST") == "" {
		t.Skip("no docker daemon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	name := "roundpen-" + bgLiveSandboxID
	_ = cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
	withInit := true
	created, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "roundpen-code-agent:local",
		Cmd:   []string{"sleep", "infinity"},
		User:  "1000:1000",
	}, &container.HostConfig{Init: &withInit}, nil, nil, name)
	if err != nil {
		t.Fatalf("create container: %v", err)
	}
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), name, container.RemoveOptions{Force: true})
	})
	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start container: %v", err)
	}

	backend, err := dockerbackend.New("", "")
	if err != nil {
		t.Fatalf("docker backend: %v", err)
	}
	defer func() { _ = backend.Close() }()

	reg := tools.NewRegistry()
	tools.RegisterShell(reg, &tools.AgentBinder{Slots: liveAgentSlots{}, Exec: liveExec{b: backend}})
	actor := tools.Actor{Username: "alice"}

	call := func(name, args string) map[string]any {
		t.Helper()
		out, err := reg.Call(ctx, actor, name, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s(%s): %v", name, args, err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("%s returned non-JSON: %s", name, out)
		}
		return payload
	}

	start := call("Bash", `{"command":"echo one; sleep 1; echo two; exit 7","run_in_background":true}`)
	id, _ := start["id"].(string)
	if len(id) != 16 {
		t.Fatalf("start=%v", start)
	}
	// Poll until the job finishes; output must accumulate across reads.
	var output strings.Builder
	first := call("BashOutput", `{"bash_id":"`+id+`"}`)
	if first["status"] != "running" {
		t.Fatalf("expected running, got %v", first)
	}
	output.WriteString(first["output"].(string))
	deadline := time.Now().Add(30 * time.Second)
	var final map[string]any
	for {
		payload := call("BashOutput", `{"bash_id":"`+id+`"}`)
		output.WriteString(payload["output"].(string))
		if payload["status"] == "exited" {
			final = payload
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never finished: %v", payload)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if final["exitCode"] != float64(7) {
		t.Fatalf("final=%v", final)
	}
	if got := output.String(); !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Fatalf("accumulated output=%q", got)
	}

	// Kill: a job that ignores TERM still dies (SIGKILL escalation).
	start2 := call("Bash", `{"command":"trap \"\" TERM; sleep 600","run_in_background":true}`)
	id2 := start2["id"].(string)
	kill := call("KillShell", `{"shell_id":"`+id2+`"}`)
	if kill["status"] != "exited" {
		t.Fatalf("kill=%v", kill)
	}
	if after := call("BashOutput", `{"bash_id":"`+id2+`"}`); after["status"] != "exited" {
		t.Fatalf("job still running after KillShell: %v", after)
	}
}
