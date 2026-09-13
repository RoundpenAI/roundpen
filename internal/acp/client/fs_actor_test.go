package client

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// managerStub mirrors sandbox.Manager's owner-scoped contract for file IO.
type managerStub struct {
	sandbox.Manager // other methods panic if called
	actor           authz.Actor
	calls           int
}

func (m *managerStub) ReadFile(ctx context.Context, id, relPath string) (io.ReadCloser, error) {
	a, ok := authz.From(ctx)
	if !ok {
		return nil, sandbox.ErrUnauthorized
	}
	m.actor, m.calls = a, m.calls+1
	return io.NopCloser(strings.NewReader("hi")), nil
}

func (m *managerStub) WriteFile(ctx context.Context, id, relPath string, r io.Reader) error {
	a, ok := authz.From(ctx)
	if !ok {
		return sandbox.ErrUnauthorized
	}
	m.actor, m.calls = a, m.calls+1
	return nil
}

// TestBridgeFileIOAttachesActor checks that ACP file requests — whose ctx comes
// from the connection read loop — reach the owner-scoped manager with the
// session actor attached.
func TestBridgeFileIOAttachesActor(t *testing.T) {
	mgr := &managerStub{}
	b := New(slog.New(slog.NewTextHandler(io.Discard, nil)), mgr, "sb-1", false, authz.Actor{Username: "alice"})

	if _, err := b.ReadTextFile(context.Background(), acp.ReadTextFileRequest{Path: "/workspace/notes.txt"}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := b.WriteTextFile(context.Background(), acp.WriteTextFileRequest{Path: "/workspace/notes.txt", Content: "x"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if mgr.calls != 2 {
		t.Fatalf("manager calls = %d, want 2", mgr.calls)
	}
	if mgr.actor != (authz.Actor{Username: "alice"}) {
		t.Fatalf("actor = %#v, want alice", mgr.actor)
	}
}
