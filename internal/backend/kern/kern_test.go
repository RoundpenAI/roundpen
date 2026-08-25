package kern_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/backend/kern"
)

func TestKernCreateExecDelete(t *testing.T) {
	dir := t.TempDir()
	be := kern.New()
	ctx := context.Background()

	id := "sb-test-1"
	engineID, err := be.Create(ctx, backend.CreateOpts{
		SandboxID: id,
		Image:     "host",
		MountDir:  dir,
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
	if !contains(out, "hello-bar") {
		t.Fatalf("stdout=%q", out)
	}

	// write into workspace via exec
	res, err = be.Exec(ctx, id, backend.ExecOpts{
		Cmd: []string{"/bin/sh", "-c", "echo hi > note.txt"},
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("write: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil || string(b) != "hi\n" {
		t.Fatalf("note.txt: %q err=%v", b, err)
	}

	// path escape
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

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
