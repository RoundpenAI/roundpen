package hostaccess_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/hostaccess"
)

type fakeSessions struct{ sess *agentsession.Session }

func (f fakeSessions) Get(context.Context, string) (*agentsession.Session, error) {
	return f.sess, nil
}

type fakeAssistants struct{ a *assistant.Assistant }

func (f fakeAssistants) Get(context.Context, string) (*assistant.Assistant, error) {
	return f.a, nil
}

func newAccess(grants []assistant.DirectoryGrant) (*hostaccess.Access, tools.Actor) {
	sess := &agentsession.Session{ID: "s1", UserID: "alice", AssistantID: "a1"}
	a := &assistant.Assistant{ID: "a1", DirectoryGrants: grants}
	return hostaccess.New(fakeSessions{sess}, fakeAssistants{a}, nil), tools.Actor{Username: "alice"}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenHostGrantedRead(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "note.md"), "nas content")

	access, actor := newAccess([]assistant.DirectoryGrant{{Path: dir, Mode: "read"}})
	host := access.ForSession("s1")
	if host == nil {
		t.Fatal("ForSession returned nil")
	}
	rc, fi, err := host.OpenHost(context.Background(), actor, filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if fi.IsDir() {
		t.Fatal("expected a file")
	}
	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "nas content" {
		t.Fatalf("content=%q", raw)
	}
}

func TestOpenHostReadwriteGrantAllowsRead(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "x")

	access, actor := newAccess([]assistant.DirectoryGrant{{Path: dir, Mode: "readwrite"}})
	if _, _, err := access.ForSession("s1").OpenHost(context.Background(), actor, filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestOpenHostDeniedWithoutGrant(t *testing.T) {
	granted := t.TempDir()
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "secret.txt"), "no")

	access, actor := newAccess([]assistant.DirectoryGrant{{Path: granted, Mode: "read"}})
	_, _, err := access.ForSession("s1").OpenHost(context.Background(), actor, filepath.Join(other, "secret.txt"))
	if err == nil || !strings.Contains(err.Error(), "host read denied") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenHostDeniedForAnotherUser(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "x")

	access, _ := newAccess([]assistant.DirectoryGrant{{Path: dir, Mode: "read"}})
	_, _, err := access.ForSession("s1").OpenHost(context.Background(), tools.Actor{Username: "bob"}, filepath.Join(dir, "a.txt"))
	if err == nil || !strings.Contains(err.Error(), "another user") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenHostDeniedWithoutAssistant(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "x")

	sess := &agentsession.Session{ID: "s1", UserID: "alice"} // no AssistantID
	access := hostaccess.New(fakeSessions{sess}, fakeAssistants{&assistant.Assistant{ID: "a1"}}, nil)
	_, _, err := access.ForSession("s1").OpenHost(context.Background(), tools.Actor{Username: "alice"}, filepath.Join(dir, "a.txt"))
	if err == nil || !strings.Contains(err.Error(), "no assistant") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenHostDeniedWhenSymlinkEscapesGrant(t *testing.T) {
	granted := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "no")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(granted, "link.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	access, actor := newAccess([]assistant.DirectoryGrant{{Path: granted, Mode: "read"}})
	_, _, err := access.ForSession("s1").OpenHost(context.Background(), actor, filepath.Join(granted, "link.txt"))
	if err == nil || !strings.Contains(err.Error(), "host read denied") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenHostDirectoryReturnsDirInfo(t *testing.T) {
	dir := t.TempDir()
	access, actor := newAccess([]assistant.DirectoryGrant{{Path: dir, Mode: "read"}})
	rc, fi, err := access.ForSession("s1").OpenHost(context.Background(), actor, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if !fi.IsDir() {
		t.Fatal("expected a directory")
	}
}

func TestGlobHostPatterns(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, "x")
	}
	mustWrite("top.md")
	mustWrite("docs/deep.md")
	mustWrite("docs/notes.txt")
	mustWrite(".hidden/skip.md")
	mustWrite(".secret.md")

	access, actor := newAccess([]assistant.DirectoryGrant{{Path: dir, Mode: "read"}})
	host := access.ForSession("s1")

	ctx := context.Background()
	got, err := host.GlobHost(ctx, actor, dir, "**/*.md", 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "docs", "deep.md"), filepath.Join(dir, "top.md")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}

	// A pattern without a separator matches file names at any depth.
	got, err = host.GlobHost(ctx, actor, dir, "*.txt", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "notes.txt" {
		t.Fatalf("got %v", got)
	}

	// The cap is honored.
	got, err = host.GlobHost(ctx, actor, dir, "**/*", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("limit ignored: %v", got)
	}
}

func TestGlobHostDeniedWithoutGrant(t *testing.T) {
	granted := t.TempDir()
	other := t.TempDir()
	access, actor := newAccess([]assistant.DirectoryGrant{{Path: granted, Mode: "read"}})
	if _, err := access.ForSession("s1").GlobHost(context.Background(), actor, other, "*", 10); err == nil {
		t.Fatal("expected denial")
	}
}

func TestNilSafety(t *testing.T) {
	if hostaccess.New(nil, fakeAssistants{&assistant.Assistant{}}, nil) != nil {
		t.Fatal("missing sessions must disable host access")
	}
	var access *hostaccess.Access
	if access.ForSession("s1") != nil {
		t.Fatal("nil accessor must return nil")
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.md", "a/b/c.md", true},
		{"**/*.md", "c.md", true},
		{"a/*", "a/b", true},
		{"a/*", "a/b/c", false},
		{"*", "a", true},
		{"*", "a/b", false},
		{"?", "a", true},
		{"[ab]x", "ax", true},
		{"[ab]x", "cx", false},
		{"docs/**", "docs/a/b/x", true},
		{"docs/**", "other/x", false},
	}
	for _, c := range cases {
		if got := hostaccess.MatchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("MatchGlob(%q, %q)=%v want %v", c.pattern, c.name, got, c.want)
		}
	}
}
