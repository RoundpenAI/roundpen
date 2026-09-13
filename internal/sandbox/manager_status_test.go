package sandbox_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// TestService_GetReconcilesDeadContainer: the store still says running after
// the container died out of band (host reboot, daemon restart); reads must
// record engine truth instead of serving the stale status.
func TestService_GetReconcilesDeadContainer(t *testing.T) {
	be := newStubBackend("docker")
	svc, store, _ := newTestService(t, be)
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{Name: "agent-admin"})
	if err != nil {
		t.Fatal(err)
	}
	be.mu.Lock()
	be.running[sb.ID] = false
	be.mu.Unlock()

	got, err := svc.Get(ctx, sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sandbox.StatusStopped {
		t.Fatalf("status = %s, want stopped", got.Status)
	}
	persisted, err := store.Get(ctx, sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != sandbox.StatusStopped {
		t.Fatalf("store status = %s, want stopped", persisted.Status)
	}
}

func TestService_ListReconcilesDeadContainer(t *testing.T) {
	be := newStubBackend("docker")
	svc, _, _ := newTestService(t, be)
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{Name: "agent-admin"})
	if err != nil {
		t.Fatal(err)
	}
	be.mu.Lock()
	be.running[sb.ID] = false
	be.mu.Unlock()

	list, err := svc.List(ctx, sandbox.ListFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}
	if list[0].Status != sandbox.StatusStopped {
		t.Fatalf("list status = %s, want stopped", list[0].Status)
	}
}

// TestService_ConnectHealsDeadContainer: connecting restarts an engine that
// died while the store still said running.
func TestService_ConnectHealsDeadContainer(t *testing.T) {
	be := newStubBackend("docker")
	svc, _, _ := newTestService(t, be)
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{Name: "agent-admin"})
	if err != nil {
		t.Fatal(err)
	}
	be.mu.Lock()
	be.running[sb.ID] = false
	be.mu.Unlock()

	got, resumed, err := svc.Connect(ctx, sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed {
		t.Fatal("connect should report the sandbox as resumed")
	}
	if got.Status != sandbox.StatusRunning {
		t.Fatalf("status = %s, want running", got.Status)
	}
	be.mu.Lock()
	restarted := be.running[sb.ID]
	be.mu.Unlock()
	if !restarted {
		t.Fatal("backend was not restarted")
	}
}
