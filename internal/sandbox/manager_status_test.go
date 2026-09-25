package sandbox_test

import (
	"testing"
	"time"

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

// TestService_GetReconcilesInterruptedCreate: a control-plane crash between the
// row insert and the engine start leaves a creating row with no container;
// reads must mark it failed so callers (userenv, the hub) rebuild the slot
// instead of adopting a sandbox that can never start.
func TestService_GetReconcilesInterruptedCreate(t *testing.T) {
	be := newStubBackend("docker")
	svc, store, _ := newTestService(t, be)
	ctx := adminCtx()

	id := "11111111-2222-3333-4444-555555555555"
	now := time.Now().UTC()
	store.byID[id] = &sandbox.Sandbox{ID: id, Status: sandbox.StatusCreating, CreatedAt: now, UpdatedAt: now}

	got, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sandbox.StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	persisted, err := store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != sandbox.StatusFailed {
		t.Fatalf("store status = %s, want failed", persisted.Status)
	}
}

// TestService_GetPromotesLiveEngineOfInterruptedCreate: when the crash happened
// after the engine came up, the same reconcile finishes the create's
// bookkeeping instead of discarding a healthy container.
func TestService_GetPromotesLiveEngineOfInterruptedCreate(t *testing.T) {
	be := newStubBackend("docker")
	svc, store, _ := newTestService(t, be)
	ctx := adminCtx()

	id := "22222222-3333-4444-5555-666666666666"
	now := time.Now().UTC()
	store.byID[id] = &sandbox.Sandbox{ID: id, Status: sandbox.StatusCreating, CreatedAt: now, UpdatedAt: now}
	be.mu.Lock()
	be.running[id] = true
	be.mu.Unlock()

	got, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sandbox.StatusRunning {
		t.Fatalf("status = %s, want running", got.Status)
	}
}

// TestService_CreateKeepsInFlightRowCreating: reconcile must not declare a
// create dead while this process is still allocating its engine, i.e. in the
// window between the store insert and the backend create returning.
func TestService_CreateKeepsInFlightRowCreating(t *testing.T) {
	be := newStubBackend("docker")
	be.createStart = make(chan struct{})
	be.createBlock = make(chan struct{})
	started, blocked := be.createStart, be.createBlock
	svc, _, _ := newTestService(t, be)
	ctx := adminCtx()

	created := make(chan *sandbox.Sandbox, 1)
	errs := make(chan error, 1)
	go func() {
		sb, err := svc.Create(ctx, sandbox.CreateRequest{ID: "mid-create", Name: "mid-create"})
		if err != nil {
			errs <- err
			return
		}
		created <- sb
	}()

	select {
	case <-started:
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("backend create never started")
	}

	got, err := svc.Get(ctx, "mid-create")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sandbox.StatusCreating {
		t.Fatalf("in-flight status = %s, want creating", got.Status)
	}

	close(blocked)
	select {
	case sb := <-created:
		if sb.Status != sandbox.StatusRunning {
			t.Fatalf("created status = %s, want running", sb.Status)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("create did not finish")
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
