package userenv

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

type memSlots struct {
	mu    sync.Mutex
	byKey map[string]Mapping
}

func (m *memSlots) key(userID, slot string) string { return userID + "/" + slot }

func (m *memSlots) Get(_ context.Context, userID, slot string) (*Mapping, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.byKey[m.key(userID, slot)]
	if !ok {
		return nil, nil
	}
	cp := got
	return &cp, nil
}

func (m *memSlots) List(_ context.Context, userID string) ([]Mapping, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Mapping
	prefix := userID + "/"
	for k, v := range m.byKey {
		if strings.HasPrefix(k, prefix) {
			out = append(out, v)
		}
	}
	return out, nil
}

func (m *memSlots) Upsert(_ context.Context, userID, slot, sandboxID, templateID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byKey == nil {
		m.byKey = map[string]Mapping{}
	}
	now := time.Now().UTC()
	key := m.key(userID, slot)
	prev := m.byKey[key]
	created := prev.CreatedAt
	if created.IsZero() {
		created = now
	}
	m.byKey[key] = Mapping{
		UserID: userID, Slot: slot, SandboxID: sandboxID, TemplateID: templateID,
		CreatedAt: created, UpdatedAt: now,
	}
	return nil
}

func (m *memSlots) Delete(_ context.Context, userID, slot string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byKey, m.key(userID, slot))
	return nil
}

type fakeSandboxes struct {
	mu         sync.Mutex
	byID       map[string]*sandbox.Sandbox
	connectErr error
	creates    int
}

func (f *fakeSandboxes) put(sb *sandbox.Sandbox) {
	if f.byID == nil {
		f.byID = map[string]*sandbox.Sandbox{}
	}
	cp := *sb
	if sb.Metadata != nil {
		cp.Metadata = map[string]string{}
		for k, v := range sb.Metadata {
			cp.Metadata[k] = v
		}
	}
	f.byID[sb.ID] = &cp
}

func (f *fakeSandboxes) clone(sb *sandbox.Sandbox) *sandbox.Sandbox {
	cp := *sb
	if sb.Metadata != nil {
		cp.Metadata = map[string]string{}
		for k, v := range sb.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp
}

func (f *fakeSandboxes) Create(_ context.Context, req sandbox.CreateRequest) (*sandbox.Sandbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := sandbox.NormalizeName(req.Name)
	for _, existing := range f.byID {
		if strings.EqualFold(existing.Name, name) {
			return nil, fmt.Errorf("%w: sandbox name already exists", sandbox.ErrConflict)
		}
	}
	f.creates++
	sb := &sandbox.Sandbox{
		ID:       fmt.Sprintf("sb-%d", f.creates),
		Name:     name,
		Category: req.Category,
		Status:   sandbox.StatusRunning,
		Metadata: req.Metadata,
	}
	f.put(sb)
	return f.clone(sb), nil
}

func (f *fakeSandboxes) Get(_ context.Context, id string) (*sandbox.Sandbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sb, ok := f.byID[id]
	if !ok {
		return nil, sandbox.ErrNotFound
	}
	return f.clone(sb), nil
}

func (f *fakeSandboxes) Resolve(_ context.Context, req sandbox.ResolveRequest) (*sandbox.Sandbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := sandbox.NormalizeName(req.Name)
	if name == "" {
		return nil, sandbox.ErrNotFound
	}
	for _, sb := range f.byID {
		if strings.EqualFold(sb.Name, name) {
			return f.clone(sb), nil
		}
	}
	return nil, sandbox.ErrNotFound
}

func (f *fakeSandboxes) Connect(_ context.Context, id string) (*sandbox.Sandbox, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connectErr != nil {
		return nil, false, f.connectErr
	}
	sb, ok := f.byID[id]
	if !ok {
		return nil, false, sandbox.ErrNotFound
	}
	resumed := sb.Status == sandbox.StatusStopped || sb.Status == sandbox.StatusPaused
	if sb.Status == sandbox.StatusFailed {
		return nil, false, fmt.Errorf("sandbox %s is %s", id, sb.Status)
	}
	sb.Status = sandbox.StatusRunning
	return f.clone(sb), resumed, nil
}

func (f *fakeSandboxes) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, id)
	return nil
}

func (f *fakeSandboxes) List(context.Context, sandbox.ListFilter) ([]*sandbox.Sandbox, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) Stop(context.Context, string) error { return fmt.Errorf("unused") }
func (f *fakeSandboxes) SetTimeout(context.Context, string, time.Duration) (*sandbox.Sandbox, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) Refresh(context.Context, string) (*sandbox.Sandbox, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) Rename(context.Context, string, string) (*sandbox.Sandbox, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) Update(context.Context, string, sandbox.UpdateRequest) (*sandbox.Sandbox, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) Exec(context.Context, string, sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) ListFiles(context.Context, string, string) ([]workspace.DirEntry, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) StatFile(context.Context, string, string) (*workspace.FileStat, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) ReadFile(context.Context, string, string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) WriteFile(context.Context, string, string, io.Reader) error {
	return fmt.Errorf("unused")
}
func (f *fakeSandboxes) RemoveFile(context.Context, string, string) error {
	return fmt.Errorf("unused")
}
func (f *fakeSandboxes) WorkspaceHostPath(context.Context, string) (string, error) {
	return "", fmt.Errorf("unused")
}
func (f *fakeSandboxes) Dial(context.Context, string, int) (net.Conn, error) {
	return nil, fmt.Errorf("unused")
}
func (f *fakeSandboxes) Touch(context.Context, string) error { return fmt.Errorf("unused") }
func (f *fakeSandboxes) AttachTerminal(context.Context, string, string, sandbox.TerminalOpts, io.Reader, io.Writer) error {
	return fmt.Errorf("unused")
}
func (f *fakeSandboxes) ResizeTerminal(context.Context, string, string, uint16, uint16) error {
	return fmt.Errorf("unused")
}
func (f *fakeSandboxes) AttachExec(context.Context, string, sandbox.AttachExecOpts, io.Reader, io.Writer, io.Writer) error {
	return fmt.Errorf("unused")
}
