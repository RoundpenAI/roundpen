package local_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/workspace/local"
)

func TestPathEscapeRejected(t *testing.T) {
	root := t.TempDir()
	fs := local.New(root)
	ctx := context.Background()
	info, err := fs.Create(ctx, "ws1", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.HostPath, "ws1") {
		t.Fatalf("host path: %s", info.HostPath)
	}
	_, err = fs.Open(ctx, "ws1", "../etc/passwd")
	if err == nil {
		t.Fatal("expected path escape error")
	}
	if err := fs.Write(ctx, "ws1", "a/b.txt", strings.NewReader("hi")); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(info.HostPath, "a", "b.txt")
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "hi" {
		t.Fatalf("file content: %q err=%v", b, err)
	}

	entries, err := fs.List(ctx, "ws1", ".")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Name == "a" && e.IsDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dir a in %#v", entries)
	}
	entries, err = fs.List(ctx, "ws1", "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "b.txt" || entries[0].IsDir {
		t.Fatalf("list a: %#v", entries)
	}
	if err := fs.RemovePath(ctx, "ws1", "a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("expected removed, err=%v", err)
	}
	if err := fs.RemovePath(ctx, "ws1", "."); err == nil {
		t.Fatal("expected refuse root remove")
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := local.New(root)
	ctx := context.Background()
	info, err := fs.Create(ctx, "ws1", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(info.HostPath, "leak")); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Open(ctx, "ws1", "leak"); err == nil {
		t.Fatal("expected symlink escape error")
	}
	if _, err := fs.Stat(ctx, "ws1", "leak"); err == nil {
		t.Fatal("expected symlink escape error")
	}
	if err := fs.RemovePath(ctx, "ws1", "leak"); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("outside file must remain: %v", err)
	}
}
