package kern_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/backend/kern"
)

func TestKernCreateExecDelete(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	be := kern.New()
	ctx := context.Background()

	id := "sb-test-1"
	engineID, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: id,
		Image:     "host",
		MountDir:  ws,
		Env:       map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if engineID == "" {
		t.Fatal("empty engine id")
	}
	if err := be.Start(ctx, id); err != nil {
		t.Fatal(err)
	}

	res, err := be.Exec(ctx, id, backend.ExecOpts{
		Cmd:     []string{"/bin/sh", "-c", "echo hello-$FOO && pwd"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	out := string(res.Stdout)
	if !strings.Contains(out, "hello-bar") {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(out, "/workspace") {
		t.Fatalf("expected pwd /workspace, stdout=%q", out)
	}

	res, err = be.Exec(ctx, id, backend.ExecOpts{
		Cmd: []string{"/bin/sh", "-c", "echo hi > note.txt"},
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("write: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
	}
	b, err := os.ReadFile(filepath.Join(ws, "note.txt"))
	if err != nil || string(b) != "hi\n" {
		t.Fatalf("note.txt: %q err=%v", b, err)
	}

	res, err = be.Exec(ctx, id, backend.ExecOpts{
		Cmd: []string{"/bin/sh", "-c", "printf '%s\\n' \"$HOME\" && pwd && hostname && touch \"$HOME/marker\" && ls /"},
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("home check: err=%v exit=%d stderr=%s stdout=%s", err, res.ExitCode, res.Stderr, res.Stdout)
	}
	lines := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected HOME, pwd, hostname lines, got %q", res.Stdout)
	}
	if lines[0] != "/home" {
		t.Fatalf("HOME=%q want /home", lines[0])
	}
	if lines[1] != "/workspace" {
		t.Fatalf("pwd=%q want /workspace", lines[1])
	}
	if !strings.HasPrefix(lines[2], "rp-") {
		t.Fatalf("hostname=%q want rp-*", lines[2])
	}

	// Named sandbox prefers a slug hostname derived from the display name.
	if _, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: id,
		Name:      "Demo Lab",
		Image:     "host",
		MountDir:  ws,
	}); err != nil {
		t.Fatal(err)
	}
	res, err = be.Exec(ctx, id, backend.ExecOpts{
		Cmd: []string{"/bin/sh", "-c", "hostname"},
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("named hostname: err=%v exit=%d", err, res.ExitCode)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "demo-lab" {
		t.Fatalf("named hostname=%q want demo-lab", got)
	}

	homeHost := filepath.Join(dir, "home")
	if _, err := os.Stat(filepath.Join(homeHost, "marker")); err != nil {
		t.Fatalf("expected marker in sandbox home: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "marker")); !os.IsNotExist(err) {
		t.Fatal("marker must not land in workspace root")
	}
	rootListing := string(res.Stdout)
	if strings.Contains(rootListing, "home/mike") || strings.Contains(rootListing+"\n", "\nmike\n") {
		t.Fatalf("guest / leaked host home entries: %q", rootListing)
	}

	// Host absolute workdirs are rejected.
	_, err = be.Exec(ctx, id, backend.ExecOpts{
		Cmd:     []string{"true"},
		WorkDir: "/tmp",
	})
	if err == nil {
		t.Fatal("expected workdir escape error")
	}

	if err := be.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	_, err = be.Exec(ctx, id, backend.ExecOpts{Cmd: []string{"true"}})
	if err == nil {
		t.Fatal("expected exec on stopped sandbox to fail")
	}
	if err := be.Remove(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestKernEnvSurvivesHydrateRecreate(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	be := kern.New()
	ctx := context.Background()
	id := "sb-env-hydrate"

	if _, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: id,
		Image:     "host",
		MountDir:  ws,
		Env:       map[string]string{"FOO": "bar"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := be.Start(ctx, id); err != nil {
		t.Fatal(err)
	}

	// Simulate sandbox.Service.hydrate: re-create without env.
	if _, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: id,
		Image:     "host",
		MountDir:  ws,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := be.Exec(ctx, id, backend.ExecOpts{
		Cmd: []string{"/bin/sh", "-c", "echo hello-$FOO"},
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("exec: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "hello-bar" {
		t.Fatalf("stdout=%q want hello-bar", got)
	}
}
