package sandbox_test

import (
	"context"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

type listStub struct {
	*stubBackend
	entries []workspace.DirEntry
	err     error
	gotRel  string
	calls   int
}

func (b *listStub) ListWorkspaceDir(_ context.Context, _ string, rel string) ([]workspace.DirEntry, error) {
	b.calls++
	b.gotRel = rel
	return b.entries, b.err
}

func newListService(t *testing.T) (*sandbox.Service, *listStub, *sandbox.Sandbox) {
	t.Helper()
	be := &listStub{stubBackend: newStubBackend("stub")}
	svc, _, _ := newTestService(t, be)
	sb, err := svc.Create(adminCtx(), sandbox.CreateRequest{TemplateID: "host"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, be, sb
}

func TestListGuestFilesFromBackend(t *testing.T) {
	svc, be, sb := newListService(t)
	be.entries = []workspace.DirEntry{{Name: "a.txt", Size: 3}, {Name: "sub", IsDir: true}}

	entries, err := svc.ListGuestFiles(adminCtx(), sb.ID, "docs")
	if err != nil {
		t.Fatalf("ListGuestFiles: %v", err)
	}
	if len(entries) != 2 || entries[0].Name != "a.txt" || !entries[1].IsDir {
		t.Fatalf("entries=%+v", entries)
	}
	if be.gotRel != "docs" {
		t.Fatalf("backend got rel %q, want docs", be.gotRel)
	}
}

func TestListGuestFilesRejectsEscape(t *testing.T) {
	svc, be, sb := newListService(t)
	if _, err := svc.ListGuestFiles(adminCtx(), sb.ID, "../etc"); err == nil {
		t.Fatal("expected escape error")
	}
	if be.calls != 0 {
		t.Fatalf("backend called %d times for rejected path", be.calls)
	}
}

func TestListGuestFilesRequiresWorkspaceCapability(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("stub"))
	sb, err := svc.Create(adminCtx(), sandbox.CreateRequest{TemplateID: "host"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListGuestFiles(adminCtx(), sb.ID, "."); err == nil {
		t.Fatal("expected capability error on backend without workspace listing")
	}
}
