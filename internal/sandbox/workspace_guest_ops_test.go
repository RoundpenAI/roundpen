package sandbox_test

import (
	"errors"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// The service probes with `test` before running mv/cp, so each case queues
// probe answers first and the operation last.
func okRes() *backend.ExecResult   { return &backend.ExecResult{ExitCode: 0} }
func failRes() *backend.ExecResult { return &backend.ExecResult{ExitCode: 1} }
func errRes(stderr string) *backend.ExecResult {
	return &backend.ExecResult{ExitCode: 1, Stderr: []byte(stderr)}
}

func newOpsService(t *testing.T) (*sandbox.Service, *stubBackend, string) {
	t.Helper()
	be := newStubBackend("docker")
	svc, _, _ := newTestService(t, be)
	sb, err := svc.Create(adminCtx(), sandbox.CreateRequest{Name: "agent-admin"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, be, sb.ID
}

func scriptExec(be *stubBackend, results ...*backend.ExecResult) {
	be.mu.Lock()
	be.execQueue = results
	be.mu.Unlock()
}

func lastCmd(calls []backend.ExecOpts) []string {
	if len(calls) == 0 {
		return nil
	}
	return calls[len(calls)-1].Cmd
}

func TestMoveGuestFileRenameInPlace(t *testing.T) {
	svc, be, id := newOpsService(t)
	scriptExec(be, failRes() /* -d: not a dir */, failRes() /* -e: free */, okRes())

	if err := svc.MoveGuestFile(adminCtx(), id, "a.txt", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	got := lastCmd(be.execCallsSnapshot())
	want := []string{"mv", "--", "/workspace/a.txt", "/workspace/renamed.txt"}
	if len(got) != len(want) {
		t.Fatalf("cmd = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cmd = %v, want %v", got, want)
		}
	}
}

func TestMoveGuestFileIntoDirectoryKeepsName(t *testing.T) {
	svc, be, id := newOpsService(t)
	scriptExec(be, okRes() /* -d: destination dir */, failRes() /* -e: free */, okRes())

	if err := svc.MoveGuestFile(adminCtx(), id, "a.txt", "sub"); err != nil {
		t.Fatal(err)
	}
	got := lastCmd(be.execCallsSnapshot())
	if got[3] != "/workspace/sub/a.txt" {
		t.Fatalf("mv target = %v", got)
	}
}

func TestMoveGuestFileConflict(t *testing.T) {
	svc, be, id := newOpsService(t)
	scriptExec(be, failRes(), okRes() /* -e: occupied */)

	err := svc.MoveGuestFile(adminCtx(), id, "a.txt", "b.txt")
	if !errors.Is(err, sandbox.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if calls := be.execCallsSnapshot(); len(calls) != 2 {
		t.Fatalf("conflict must stop before mv, calls = %d", len(calls))
	}
}

func TestMoveGuestFileIntoItself(t *testing.T) {
	svc, be, id := newOpsService(t)
	scriptExec(be, failRes() /* -d: destination does not exist */)

	err := svc.MoveGuestFile(adminCtx(), id, "a", "a/b")
	if err == nil {
		t.Fatal("expected an error when placing a directory inside itself")
	}
	if calls := be.execCallsSnapshot(); len(calls) != 1 {
		t.Fatalf("must reject before moving, calls = %d", len(calls))
	}
}

func TestMoveGuestFileSamePlaceIsNoop(t *testing.T) {
	svc, be, id := newOpsService(t)
	scriptExec(be, okRes() /* "." is a dir → target equals source */)

	if err := svc.MoveGuestFile(adminCtx(), id, "a.txt", "."); err != nil {
		t.Fatal(err)
	}
	if calls := be.execCallsSnapshot(); len(calls) != 1 {
		t.Fatalf("no-op must not run mv, calls = %d", len(calls))
	}
}

func TestCopyGuestFileOntoItselfConflicts(t *testing.T) {
	svc, be, id := newOpsService(t)
	scriptExec(be, failRes() /* -d: destination is the same file */)

	err := svc.CopyGuestFile(adminCtx(), id, "a.txt", "a.txt")
	if !errors.Is(err, sandbox.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if calls := be.execCallsSnapshot(); len(calls) != 1 {
		t.Fatalf("must reject before copying, calls = %d", len(calls))
	}
}

func TestCopyGuestFileUsesCpA(t *testing.T) {
	svc, be, id := newOpsService(t)
	scriptExec(be, failRes(), failRes(), okRes())

	if err := svc.CopyGuestFile(adminCtx(), id, "a.txt", "copy.txt"); err != nil {
		t.Fatal(err)
	}
	got := lastCmd(be.execCallsSnapshot())
	if got[0] != "cp" || got[1] != "-a" || got[2] != "--" {
		t.Fatalf("cp cmd = %v", got)
	}
}

func TestRelocateRejectsRootAndEscape(t *testing.T) {
	svc, be, id := newOpsService(t)

	if err := svc.MoveGuestFile(adminCtx(), id, ".", "x"); err == nil {
		t.Fatal("moving the workspace root must fail")
	}
	if err := svc.MoveGuestFile(adminCtx(), id, "a.txt", "../x"); err == nil {
		t.Fatal("escaping destinations must fail")
	}
	if calls := be.execCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("validation must happen before exec, calls = %d", len(calls))
	}
}

func TestRelocateClassifiesExecStderr(t *testing.T) {
	svc, be, id := newOpsService(t)
	scriptExec(be, failRes(), failRes(),
		errRes("mv: cannot stat 'a.txt': No such file or directory"))
	if err := svc.MoveGuestFile(adminCtx(), id, "a.txt", "b.txt"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("stderr no-such-file → %v", err)
	}

	svc, be, id = newOpsService(t)
	scriptExec(be, failRes(), failRes(), errRes("mv: cannot move 'a.txt' to 'b.txt': File exists"))
	if err := svc.MoveGuestFile(adminCtx(), id, "a.txt", "b.txt"); !errors.Is(err, sandbox.ErrConflict) {
		t.Fatalf("stderr exists → %v", err)
	}
}
