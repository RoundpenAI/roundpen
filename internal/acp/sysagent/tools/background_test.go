package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func backgroundRegistry(t *testing.T, ex *stubExec) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	tools.RegisterShell(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb-agent"},
		Exec:  ex,
		Files: &memFiles{data: map[string][]byte{}},
	})
	return reg
}

func callJSON(t *testing.T, reg *tools.Registry, name, args string) map[string]any {
	t.Helper()
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("%s returned non-JSON: %s", name, out)
	}
	return payload
}

func TestBashRunInBackground(t *testing.T) {
	ex := &stubExec{ws: t.TempDir()}
	reg := backgroundRegistry(t, ex)

	payload := callJSON(t, reg, "Bash", `{"command":"make -j8","workdir":"sub","run_in_background":true}`)
	id, _ := payload["id"].(string)
	if len(id) != 16 || strings.Trim(id, "0123456789abcdef") != "" {
		t.Fatalf("id=%q", id)
	}
	if payload["status"] != "running" {
		t.Fatalf("status=%v", payload["status"])
	}
	if len(ex.cmd) != 3 || ex.cmd[0] != "/bin/sh" {
		t.Fatalf("cmd=%q", ex.cmd)
	}
	script := ex.cmd[2]
	if !strings.Contains(script, "/tmp/roundpen-jobs/"+id) || !strings.Contains(script, "setsid") {
		t.Fatalf("script=%s", script)
	}
	// The command itself travels via env so it never reaches a shell parser.
	if ex.env["ROUNDPEN_BG_CMD"] != "make -j8" {
		t.Fatalf("env cmd=%q", ex.env["ROUNDPEN_BG_CMD"])
	}
	if ex.env["ROUNDPEN_BG_WORKDIR"] != "/workspace/sub" {
		t.Fatalf("env workdir=%q", ex.env["ROUNDPEN_BG_WORKDIR"])
	}
	if ex.env["GIT_TERMINAL_PROMPT"] != "0" {
		t.Fatalf("env=%v", ex.env)
	}
}

func TestBashRunInBackgroundLaunchFailure(t *testing.T) {
	ex := &stubExec{
		ws:  t.TempDir(),
		res: &sandbox.ExecResult{ExitCode: 1, Stderr: []byte("workdir not found: /nope\n")},
	}
	reg := backgroundRegistry(t, ex)
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Bash",
		json.RawMessage(`{"command":"echo hi","workdir":"/nope","run_in_background":true}`))
	if err == nil || !strings.Contains(err.Error(), "workdir not found") {
		t.Fatalf("err=%v", err)
	}
}

func TestBashOutputReadsStatusAndOutput(t *testing.T) {
	ex := &stubExec{
		ws: t.TempDir(),
		queue: []*sandbox.ExecResult{
			{ExitCode: 0, Stdout: []byte("STATUS exited\nEXIT 3\nSIZE 11\nOFFSET 8\n---\ndone\n")},
		},
	}
	reg := backgroundRegistry(t, ex)

	payload := callJSON(t, reg, "BashOutput", `{"bash_id":"0123456789abcdef"}`)
	if payload["status"] != "exited" || payload["exitCode"] != float64(3) {
		t.Fatalf("payload=%v", payload)
	}
	if payload["output"] != "done\n" || payload["bytesRemaining"] != float64(0) {
		t.Fatalf("payload=%v", payload)
	}
	if !strings.Contains(ex.cmd[2], "/tmp/roundpen-jobs/0123456789abcdef") {
		t.Fatalf("cmd=%q", ex.cmd)
	}
}

func TestBashOutputRunningWithBufferedOutput(t *testing.T) {
	ex := &stubExec{
		ws: t.TempDir(),
		queue: []*sandbox.ExecResult{
			{ExitCode: 0, Stdout: []byte("STATUS running\nEXIT -\nSIZE 100\nOFFSET 10\n---\nxxxxx")},
		},
	}
	reg := backgroundRegistry(t, ex)
	payload := callJSON(t, reg, "BashOutput", `{"bash_id":"0123456789abcdef"}`)
	if payload["status"] != "running" {
		t.Fatalf("payload=%v", payload)
	}
	if _, ok := payload["exitCode"]; ok {
		t.Fatalf("exitCode must be absent while running: %v", payload)
	}
	if payload["bytesRemaining"] != float64(85) {
		t.Fatalf("payload=%v", payload)
	}
	if note, _ := payload["note"].(string); !strings.Contains(note, "More output") {
		t.Fatalf("note=%v", payload["note"])
	}
}

func TestBashOutputUnknownJob(t *testing.T) {
	ex := &stubExec{ws: t.TempDir(), res: &sandbox.ExecResult{Stdout: []byte("STATUS missing\n")}}
	reg := backgroundRegistry(t, ex)
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "BashOutput",
		json.RawMessage(`{"bash_id":"0123456789abcdef"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown background job") {
		t.Fatalf("err=%v", err)
	}
}

func TestBackgroundToolsRejectMalformedIDs(t *testing.T) {
	bad := []string{
		`{"bash_id":"zz"}`,
		`{"bash_id":"0123456789abcde; rm -rf /"}`,
		`{"bash_id":"../../etc/passwd"}`,
		`{"bash_id":"0123456789ABCDEF"}`,
		`{}`,
	}
	for _, args := range bad {
		ex := &stubExec{ws: t.TempDir()}
		reg := backgroundRegistry(t, ex)
		if _, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "BashOutput", json.RawMessage(args)); err == nil {
			t.Fatalf("BashOutput accepted %s", args)
		}
		if ex.cmd != nil {
			t.Fatalf("BashOutput executed for %s: %q", args, ex.cmd)
		}
	}
	ex := &stubExec{ws: t.TempDir()}
	reg := backgroundRegistry(t, ex)
	if _, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "KillShell",
		json.RawMessage(`{"shell_id":"not-an-id"}`)); err == nil {
		t.Fatal("KillShell accepted a malformed id")
	}
	if ex.cmd != nil {
		t.Fatalf("KillShell executed: %q", ex.cmd)
	}
}

func TestKillShell(t *testing.T) {
	ex := &stubExec{
		ws: t.TempDir(),
		queue: []*sandbox.ExecResult{
			{ExitCode: 0, Stdout: []byte("STATUS exited\nEXIT -\n")},
		},
	}
	reg := backgroundRegistry(t, ex)
	payload := callJSON(t, reg, "KillShell", `{"shell_id":"fedcba9876543210"}`)
	if payload["status"] != "exited" {
		t.Fatalf("payload=%v", payload)
	}
	if !strings.Contains(ex.cmd[2], "/tmp/roundpen-jobs/fedcba9876543210") {
		t.Fatalf("cmd=%q", ex.cmd)
	}
}

func TestBackgroundToolVisibility(t *testing.T) {
	ex := &stubExec{ws: t.TempDir()}
	reg := backgroundRegistry(t, ex)
	for _, name := range []string{"Bash", "BashOutput", "KillShell"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		lower := strings.ToLower(tool.Description)
		if strings.Contains(lower, "sandbox") || strings.Contains(lower, "guest") {
			t.Fatalf("%s description leaks internals: %s", name, tool.Description)
		}
	}
	if out, ok := reg.Get("BashOutput"); !ok || out.Mutating {
		t.Fatal("BashOutput must be read-only")
	}
	if kill, ok := reg.Get("KillShell"); !ok || !kill.Mutating {
		t.Fatal("KillShell must be mutating")
	}
}
