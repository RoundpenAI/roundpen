package sandbox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/workspace/local"
)

func adminCtx() context.Context {
	return authz.WithActor(context.Background(), authz.Actor{Username: "admin", Admin: true})
}

func userCtx(name string) context.Context {
	return authz.WithActor(context.Background(), authz.Actor{Username: name})
}

type memStore struct {
	mu      sync.Mutex
	byID    map[string]*sandbox.Sandbox
	deleted map[string]bool
}

func newMemStore() *memStore {
	return &memStore{
		byID:    map[string]*sandbox.Sandbox{},
		deleted: map[string]bool{},
	}
}

func (m *memStore) clone(sb *sandbox.Sandbox) *sandbox.Sandbox {
	cp := *sb
	if sb.Metadata != nil {
		cp.Metadata = make(map[string]string, len(sb.Metadata))
		for k, v := range sb.Metadata {
			cp.Metadata[k] = v
		}
	}
	if sb.ExpiresAt != nil {
		t := *sb.ExpiresAt
		cp.ExpiresAt = &t
	}
	return &cp
}

func (m *memStore) Insert(_ context.Context, sb *sandbox.Sandbox) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.byID {
		if m.deleted[existing.ID] {
			continue
		}
		if strings.EqualFold(existing.Name, sb.Name) && existing.Owner == sb.Owner {
			return sandbox.ErrConflict
		}
	}
	m.byID[sb.ID] = m.clone(sb)
	return nil
}

func (m *memStore) Update(_ context.Context, sb *sandbox.Sandbox) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleted[sb.ID] {
		return sandbox.ErrNotFound
	}
	for _, existing := range m.byID {
		if m.deleted[existing.ID] || existing.ID == sb.ID {
			continue
		}
		if strings.EqualFold(existing.Name, sb.Name) && existing.Owner == sb.Owner {
			return sandbox.ErrConflict
		}
	}
	m.byID[sb.ID] = m.clone(sb)
	return nil
}

func (m *memStore) SoftDelete(_ context.Context, id string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[id]; !ok {
		return sandbox.ErrNotFound
	}
	m.deleted[id] = true
	return nil
}

func (m *memStore) Get(_ context.Context, id string) (*sandbox.Sandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleted[id] {
		return nil, sandbox.ErrNotFound
	}
	sb, ok := m.byID[id]
	if !ok {
		return nil, sandbox.ErrNotFound
	}
	return m.clone(sb), nil
}

func (m *memStore) GetByName(_ context.Context, name, owner string) (*sandbox.Sandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sb := range m.byID {
		if m.deleted[sb.ID] {
			continue
		}
		if !strings.EqualFold(sb.Name, name) {
			continue
		}
		if owner != "" && sb.Owner != owner {
			continue
		}
		return m.clone(sb), nil
	}
	return nil, sandbox.ErrNotFound
}

func (m *memStore) GetDefaultByCategory(_ context.Context, category, owner string) (*sandbox.Sandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sb := range m.byID {
		if m.deleted[sb.ID] {
			continue
		}
		if sb.Category != category || !sb.IsDefault {
			continue
		}
		if owner != "" && sb.Owner != owner {
			continue
		}
		return m.clone(sb), nil
	}
	return nil, sandbox.ErrNotFound
}

func (m *memStore) List(_ context.Context) ([]*sandbox.Sandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*sandbox.Sandbox
	for id, sb := range m.byID {
		if m.deleted[id] {
			continue
		}
		out = append(out, m.clone(sb))
	}
	return out, nil
}

func (m *memStore) ListByCategory(_ context.Context, category string) ([]*sandbox.Sandbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*sandbox.Sandbox
	for id, sb := range m.byID {
		if m.deleted[id] || sb.Category != category {
			continue
		}
		out = append(out, m.clone(sb))
	}
	return out, nil
}

func (m *memStore) ClearDefaultInCategory(_ context.Context, category, exceptID, owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, sb := range m.byID {
		if m.deleted[id] || sb.Category != category || sb.ID == exceptID {
			continue
		}
		if owner != "" && sb.Owner != owner {
			continue
		}
		sb.IsDefault = false
	}
	return nil
}

type stubBackend struct {
	name      string
	mu        sync.Mutex
	created   map[string]backend.CreateOpts
	running   map[string]bool
	createErr error
	startErr  error
	execRes   *backend.ExecResult
	execErr   error
}

func newStubBackend(name string) *stubBackend {
	return &stubBackend{
		name:    name,
		created: map[string]backend.CreateOpts{},
		running: map[string]bool{},
		execRes: &backend.ExecResult{ExitCode: 0, Stdout: []byte("ok")},
	}
}

func (b *stubBackend) Name() string { return b.name }

func (b *stubBackend) Create(_ context.Context, opts backend.CreateOpts) (string, error) {
	if b.createErr != nil {
		return "", b.createErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.created[opts.SandboxID] = opts
	return "stub:" + opts.SandboxID, nil
}

func (b *stubBackend) Start(_ context.Context, sandboxID string) error {
	if b.startErr != nil {
		return b.startErr
	}
	b.mu.Lock()
	b.running[sandboxID] = true
	b.mu.Unlock()
	return nil
}

func (b *stubBackend) Stop(_ context.Context, sandboxID string) error {
	b.mu.Lock()
	b.running[sandboxID] = false
	b.mu.Unlock()
	return nil
}

func (b *stubBackend) Remove(_ context.Context, sandboxID string) error {
	b.mu.Lock()
	delete(b.running, sandboxID)
	delete(b.created, sandboxID)
	b.mu.Unlock()
	return nil
}

func (b *stubBackend) Exec(_ context.Context, _ string, _ backend.ExecOpts) (*backend.ExecResult, error) {
	if b.execErr != nil {
		return nil, b.execErr
	}
	return b.execRes, nil
}

func (b *stubBackend) Logs(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (b *stubBackend) Dial(_ context.Context, _ string, _ int) (net.Conn, error) {
	return nil, errors.New("dial not implemented")
}

func (b *stubBackend) AttachPTY(_ context.Context, _, _ string, _ backend.PTYOpts, _ io.Reader, _ io.Writer) error {
	return nil
}

func (b *stubBackend) ResizePTY(_ context.Context, _, _ string, _, _ uint16) error {
	return nil
}

func newTestService(t *testing.T, be backend.Backend) (*sandbox.Service, *memStore, string) {
	t.Helper()
	root := t.TempDir()
	store := newMemStore()
	svc := sandbox.NewService(store, be, local.New(root), "host", 10*time.Minute, nil)
	return svc, store, root
}

func TestNewService_DefaultLogger(t *testing.T) {
	svc := sandbox.NewService(newMemStore(), newStubBackend("stub"), local.New(t.TempDir()), "host", time.Minute, nil)
	if svc == nil {
		t.Fatal("nil service")
	}
}

func TestService_CreateGetListDelete(t *testing.T) {
	be := newStubBackend("stub")
	svc, _, _ := newTestService(t, be)
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{
		Name:       "  Lab One  ",
		Category:   "Code",
		IsDefault:  true,
		TemplateID: "host",
		Env:        map[string]string{"FOO": "bar"},
		Metadata:   map[string]string{"team": "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sb.Status != sandbox.StatusRunning || sb.Name != "Lab One" || sb.Category != "Code" || !sb.IsDefault {
		t.Fatalf("create: %#v", sb)
	}
	if sb.Metadata["templateID"] != "host" {
		t.Fatalf("metadata=%v", sb.Metadata)
	}

	got, err := svc.Get(ctx, sb.ID)
	if err != nil || got.Name != "Lab One" {
		t.Fatalf("get: err=%v sb=%#v", err, got)
	}

	list, err := svc.List(ctx, sandbox.ListFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	byCat, err := svc.List(ctx, sandbox.ListFilter{Category: "Code"})
	if err != nil || len(byCat) != 1 {
		t.Fatalf("list by category: err=%v len=%d", err, len(byCat))
	}

	res, err := svc.Exec(ctx, sb.ID, sandbox.ExecRequest{Cmd: []string{"true"}})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("exec: err=%v res=%#v", err, res)
	}

	host, err := svc.WorkspaceHostPath(ctx, sb.ID)
	if err != nil || host == "" {
		t.Fatalf("workspace path: err=%v host=%q", err, host)
	}

	if err := svc.Stop(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	stopped, _ := svc.Get(ctx, sb.ID)
	if stopped.Status != sandbox.StatusStopped {
		t.Fatalf("status=%s", stopped.Status)
	}

	if _, err := svc.Exec(ctx, sb.ID, sandbox.ExecRequest{Cmd: []string{"true"}}); err == nil {
		t.Fatal("expected exec on stopped sandbox to fail")
	}

	if err := svc.Delete(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, sb.ID); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("after delete: err=%v", err)
	}
}

func TestService_CreateNameConflict(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))
	ctx := adminCtx()
	if _, err := svc.Create(ctx, sandbox.CreateRequest{Name: "dup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, sandbox.CreateRequest{Name: "dup"}); !errors.Is(err, sandbox.ErrConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestService_CreateBackendFailures(t *testing.T) {
	ctx := adminCtx()

	be := newStubBackend("stub")
	be.createErr = errors.New("create failed")
	svc, _, _ := newTestService(t, be)
	if _, err := svc.Create(ctx, sandbox.CreateRequest{}); err == nil {
		t.Fatal("expected create error")
	}

	be2 := newStubBackend("stub")
	be2.startErr = errors.New("start failed")
	svc2, _, _ := newTestService(t, be2)
	if _, err := svc2.Create(ctx, sandbox.CreateRequest{}); err == nil {
		t.Fatal("expected start error")
	}
}

func TestService_Resolve(t *testing.T) {
	svc, store, _ := newTestService(t, newStubBackend("stub"))
	ctx := adminCtx()

	a, err := svc.Create(ctx, sandbox.CreateRequest{Name: "alpha", Category: "Browser"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Create(ctx, sandbox.CreateRequest{Name: "beta", Category: "Browser"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Resolve(ctx, sandbox.ResolveRequest{Name: "alpha"})
	if err != nil || got.ID != a.ID {
		t.Fatalf("by name: err=%v id=%s", err, got.ID)
	}

	if _, err := svc.Resolve(ctx, sandbox.ResolveRequest{Category: "Missing"}); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("missing category: err=%v", err)
	}

	if _, err := svc.Resolve(ctx, sandbox.ResolveRequest{}); err == nil {
		t.Fatal("expected error for empty resolve")
	}

	// Fallback to first in category when no default is set.
	got, err = svc.Resolve(ctx, sandbox.ResolveRequest{Category: "Browser"})
	if err != nil || (got.ID != a.ID && got.ID != b.ID) {
		t.Fatalf("category fallback: err=%v id=%s", err, got.ID)
	}

	// Explicit default wins.
	store.mu.Lock()
	store.byID[b.ID].IsDefault = true
	store.byID[b.ID].Category = "Browser"
	store.mu.Unlock()
	got, err = svc.Resolve(ctx, sandbox.ResolveRequest{Category: "Browser"})
	if err != nil || got.ID != b.ID {
		t.Fatalf("default: err=%v id=%s want %s", err, got.ID, b.ID)
	}
}

func TestService_UpdateRenameTimeoutTouch(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("kern"))
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{Name: "old", Category: "Code"})
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := svc.Rename(ctx, sb.ID, "  new-name  ")
	if err != nil || renamed.Name != "new-name" {
		t.Fatalf("rename: err=%v name=%q", err, renamed.Name)
	}

	isDefault := true
	updated, err := svc.Update(ctx, sb.ID, sandbox.UpdateRequest{
		Category:  strPtr("Tools"),
		IsDefault: &isDefault,
	})
	if err != nil || updated.Category != "Tools" || !updated.IsDefault {
		t.Fatalf("update: err=%v sb=%#v", err, updated)
	}

	if _, err := svc.Update(ctx, sb.ID, sandbox.UpdateRequest{Name: strPtr("   ")}); err == nil {
		t.Fatal("expected empty name error")
	}

	withTTL, err := svc.SetTimeout(ctx, sb.ID, 30*time.Minute)
	if err != nil || withTTL.TTLSeconds != 1800 {
		t.Fatalf("timeout: err=%v ttl=%d", err, withTTL.TTLSeconds)
	}

	before := withTTL.LastActiveAt
	time.Sleep(5 * time.Millisecond)
	if err := svc.Touch(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Get(ctx, sb.ID)
	if !after.LastActiveAt.After(before) {
		t.Fatal("touch did not advance last active")
	}
}

func TestService_FileOps(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.WriteFile(ctx, sb.ID, "dir/note.txt", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	entries, err := svc.ListFiles(ctx, sb.ID, "dir")
	if err != nil || len(entries) != 1 || entries[0].Name != "note.txt" {
		t.Fatalf("list: err=%v entries=%#v", err, entries)
	}
	rc, err := svc.ReadFile(ctx, sb.ID, "dir/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(body) != "hello" {
		t.Fatalf("read=%q", body)
	}
	if err := svc.RemoveFile(ctx, sb.ID, "dir/note.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestService_HydrateKernBackend(t *testing.T) {
	be := newStubBackend("kern")
	svc, _, _ := newTestService(t, be)
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{Name: "hydrate-me"})
	if err != nil {
		t.Fatal(err)
	}
	be.mu.Lock()
	delete(be.running, sb.ID)
	delete(be.created, sb.ID)
	be.mu.Unlock()

	if _, err := svc.Exec(ctx, sb.ID, sandbox.ExecRequest{Cmd: []string{"true"}}); err != nil {
		t.Fatalf("exec after hydrate: %v", err)
	}
	be.mu.Lock()
	_, ok := be.created[sb.ID]
	be.mu.Unlock()
	if !ok {
		t.Fatal("hydrate should recreate backend instance")
	}
}

func TestService_DialRequiresRunning(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))
	ctx := adminCtx()
	sb, err := svc.Create(ctx, sandbox.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Stop(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Dial(ctx, sb.ID, 8080); err == nil {
		t.Fatal("expected dial failure on stopped sandbox")
	}
}

func TestService_GetFillDefaultName(t *testing.T) {
	store := newMemStore()
	now := time.Now().UTC()
	id := "11111111-2222-3333-4444-555555555555"
	store.byID[id] = &sandbox.Sandbox{
		ID: id, Status: sandbox.StatusRunning, WorkspaceID: id,
		CreatedAt: now, UpdatedAt: now,
	}
	svc := sandbox.NewService(store, newStubBackend("stub"), local.New(t.TempDir()), "host", time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	got, err := svc.Get(adminCtx(), id)
	if err != nil || got.Name == "" || !strings.HasPrefix(got.Name, "sandbox-") {
		t.Fatalf("get: err=%v name=%q", err, got.Name)
	}
}

func TestService_OwnerIsolation(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))

	alice := userCtx("alice")
	bob := userCtx("bob")
	sb, err := svc.Create(alice, sandbox.CreateRequest{Name: "alice-box"})
	if err != nil {
		t.Fatal(err)
	}
	if sb.Owner != "alice" {
		t.Fatalf("owner=%q", sb.Owner)
	}

	if _, err := svc.Get(bob, sb.ID); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("bob get: %v", err)
	}
	if _, err := svc.Exec(bob, sb.ID, sandbox.ExecRequest{Cmd: []string{"true"}}); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("bob exec: %v", err)
	}
	if err := svc.Delete(bob, sb.ID); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("bob delete: %v", err)
	}
	list, err := svc.List(bob, sandbox.ListFilter{})
	if err != nil || len(list) != 0 {
		t.Fatalf("bob list: err=%v len=%d", err, len(list))
	}

	got, err := svc.Get(alice, sb.ID)
	if err != nil || got.ID != sb.ID {
		t.Fatalf("alice get: err=%v", err)
	}
	adminList, err := svc.List(adminCtx(), sandbox.ListFilter{})
	if err != nil || len(adminList) != 1 {
		t.Fatalf("admin list: err=%v len=%d", err, len(adminList))
	}
	if _, err := svc.Create(context.Background(), sandbox.CreateRequest{}); !errors.Is(err, sandbox.ErrUnauthorized) {
		t.Fatalf("no actor: %v", err)
	}
}

func TestService_DeleteKeepsPersistentWorkspace(t *testing.T) {
	root := t.TempDir()
	fs := local.New(root)
	store := newMemStore()
	svc := sandbox.NewService(store, newStubBackend("stub"), fs, "host", time.Minute, nil)
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{WorkspaceID: "shared-ws"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Get(ctx, "shared-ws"); err != nil {
		t.Fatalf("persistent workspace removed: %v", err)
	}
}

func TestService_AttachAndResizeTerminal(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("kern"))
	ctx := adminCtx()
	sb, err := svc.Create(ctx, sandbox.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachTerminal(ctx, sb.ID, "sess1", sandbox.TerminalOpts{}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResizeTerminal(ctx, sb.ID, "sess1", 40, 120); err != nil {
		t.Fatal(err)
	}
}

func TestService_UpdateConflictAndClearDefault(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("kern"))
	ctx := adminCtx()

	first, err := svc.Create(ctx, sandbox.CreateRequest{Name: "first", Category: "Lab", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(ctx, sandbox.CreateRequest{Name: "second", Category: "Lab"})
	if err != nil {
		t.Fatal(err)
	}

	isDefault := true
	if _, err := svc.Update(ctx, second.ID, sandbox.UpdateRequest{IsDefault: &isDefault}); err != nil {
		t.Fatal(err)
	}
	updatedFirst, _ := svc.Get(ctx, first.ID)
	if updatedFirst.IsDefault {
		t.Fatal("first should no longer be default")
	}

	if _, err := svc.Update(ctx, second.ID, sandbox.UpdateRequest{Name: strPtr("first")}); !errors.Is(err, sandbox.ErrConflict) {
		t.Fatalf("update conflict: err=%v", err)
	}
}

func TestService_MountPathFromWorkspaceFS(t *testing.T) {
	store := newMemStore()
	root := t.TempDir()
	fs := local.New(root)
	svc := sandbox.NewService(store, newStubBackend("stub"), fs, "host", time.Minute, nil)
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.byID[sb.ID].WorkspacePath = ""
	store.mu.Unlock()

	path, err := svc.WorkspaceHostPath(ctx, sb.ID)
	if err != nil || path == "" {
		t.Fatalf("mount path: err=%v path=%q", err, path)
	}
}

func TestService_SetTimeoutUsesDefault(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))
	ctx := adminCtx()
	sb, err := svc.Create(ctx, sandbox.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.SetTimeout(ctx, sb.ID, 0)
	if err != nil || got.TTLSeconds != 600 {
		t.Fatalf("timeout: err=%v ttl=%d", err, got.TTLSeconds)
	}
}

func TestService_NotFoundAndMissingWorkspace(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))
	ctx := adminCtx()
	if _, err := svc.Get(ctx, "missing"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("get missing: err=%v", err)
	}

	store := newMemStore()
	now := time.Now().UTC()
	id := "no-ws"
	store.byID[id] = &sandbox.Sandbox{ID: id, Status: sandbox.StatusRunning, CreatedAt: now, UpdatedAt: now}
	svc2 := sandbox.NewService(store, newStubBackend("stub"), local.New(t.TempDir()), "host", time.Minute, nil)
	if _, err := svc2.ListFiles(ctx, id, "."); err == nil {
		t.Fatal("expected missing workspace error")
	}
}

func TestService_ConnectResumeAndRefresh(t *testing.T) {
	be := newStubBackend("kern")
	svc, _, _ := newTestService(t, be)
	ctx := adminCtx()

	sb, err := svc.Create(ctx, sandbox.CreateRequest{TemplateID: "host"})
	if err != nil {
		t.Fatal(err)
	}

	got, resumed, err := svc.Connect(ctx, sb.ID)
	if err != nil || resumed || got.Status != sandbox.StatusRunning {
		t.Fatalf("connect running: err=%v resumed=%v status=%s", err, resumed, got.Status)
	}

	if err := svc.Stop(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}

	got, resumed, err = svc.Connect(ctx, sb.ID)
	if err != nil || !resumed || got.Status != sandbox.StatusRunning {
		t.Fatalf("connect resume: err=%v resumed=%v status=%s", err, resumed, got.Status)
	}

	before, _ := svc.Get(ctx, sb.ID)
	if _, err := svc.Refresh(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Get(ctx, sb.ID)
	if !after.LastActiveAt.After(before.LastActiveAt) {
		t.Fatal("refresh should touch sandbox")
	}
}

func TestService_StatFile(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))
	ctx := adminCtx()
	sb, err := svc.Create(ctx, sandbox.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.WriteFile(ctx, sb.ID, "note.txt", strings.NewReader("hi")); err != nil {
		t.Fatal(err)
	}
	st, err := svc.StatFile(ctx, sb.ID, "note.txt")
	if err != nil || st.Name != "note.txt" || st.Size != 2 {
		t.Fatalf("stat: err=%v st=%#v", err, st)
	}
}

func TestService_StopKeepsFilesAccessible(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))
	ctx := adminCtx()
	sb, err := svc.Create(ctx, sandbox.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.WriteFile(ctx, sb.ID, "keep.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if err := svc.Stop(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Exec(ctx, sb.ID, sandbox.ExecRequest{Cmd: []string{"true"}}); err == nil {
		t.Fatal("exec should fail when stopped")
	}
	rc, err := svc.ReadFile(ctx, sb.ID, "keep.txt")
	if err != nil {
		t.Fatalf("files should work when stopped: %v", err)
	}
	_ = rc.Close()
}

func strPtr(s string) *string { return &s }

func TestService_CreateWithTemplateResolver(t *testing.T) {
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := adminCtx()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tplStore := template.NewStore(db.SQL)
	tplSvc := template.NewService(tplStore, "host")
	if err := tplSvc.Seed(ctx, "stub"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	be := newStubBackend("stub")
	root := t.TempDir()
	store := newMemStore()
	svc := sandbox.NewService(store, be, local.New(root), "host", 10*time.Minute, nil, sandbox.WithTemplates(tplSvc))

	sb, err := svc.Create(ctx, sandbox.CreateRequest{TemplateID: "python"})
	if err != nil {
		t.Fatal(err)
	}
	if sb.CPUCount != 1 || sb.MemoryMB != 1024 || sb.Image != "python:3.12-slim" {
		t.Fatalf("sandbox=%+v", sb)
	}
	if sb.Metadata["templateID"] != "python" {
		t.Fatalf("metadata=%v", sb.Metadata)
	}

	be.mu.Lock()
	opts := be.created[sb.ID]
	be.mu.Unlock()
	if opts.Image != "python:3.12-slim" {
		t.Fatalf("backend image=%q", opts.Image)
	}
	if opts.CPULimit != 1 || opts.MemoryLimit != int64(1024)*1024*1024 {
		t.Fatalf("backend limits cpu=%v mem=%v", opts.CPULimit, opts.MemoryLimit)
	}
}

var (
	_ sandbox.Store   = (*memStore)(nil)
	_ backend.Backend = (*stubBackend)(nil)
)
