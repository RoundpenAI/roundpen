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
}
