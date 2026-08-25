package sshfs_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/workspace/sshfs"
)

// memRunner is a tiny in-memory SSH stand-in for unit tests.
type memRunner struct {
	dirs  map[string]struct{}
	files map[string][]byte
}

func newMem() *memRunner {
	return &memRunner{
		dirs:  map[string]struct{}{"/var/lib/roundpen": {}},
		files: map[string][]byte{},
	}
}

func (m *memRunner) Run(ctx context.Context, script string, stdin io.Reader) ([]byte, error) {
	_ = ctx
	switch {
	case strings.Contains(script, "mkdir -p") && strings.Contains(script, "chmod") && strings.Contains(script, "cat >"):
		path := between(script, "cat > '", "'")
		dir := path[:strings.LastIndex(path, "/")]
		m.dirs[dir] = struct{}{}
		b, _ := io.ReadAll(stdin)
		m.files[path] = b
		return nil, nil
	case strings.Contains(script, "mkdir -p") && strings.Contains(script, "chmod"):
		// create: last quoted path is workspace dir
		parts := strings.Split(script, "'")
		for i := 1; i < len(parts); i += 2 {
			m.dirs[parts[i]] = struct{}{}
		}
		return nil, nil
	case strings.HasPrefix(strings.TrimSpace(script), "if [ -d"):
		p := between(script, "if [ -d '", "'")
		if _, ok := m.dirs[p]; ok {
			return []byte("OK\n"), nil
		}
		return []byte("MISSING\n"), nil
	case strings.HasPrefix(strings.TrimSpace(script), "rm -rf"):
		p := between(script, "rm -rf '", "'")
		delete(m.dirs, p)
		for fp := range m.files {
			if strings.HasPrefix(fp, p+"/") || fp == p {
				delete(m.files, fp)
			}
		}
		return nil, nil
	case strings.HasPrefix(strings.TrimSpace(script), "cat '") || strings.HasPrefix(strings.TrimSpace(script), "cat "):
		p := between(script, "cat '", "'")
		b, ok := m.files[p]
		if !ok {
			return nil, fmt.Errorf("no such file")
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unhandled: %s", script)
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	i += len(a)
	j := strings.Index(s[i:], b)
	if j < 0 {
		return s[i:]
	}
	return s[i : i+j]
}

func TestSSHFSCreateWriteOpenEscapeRemove(t *testing.T) {
	fs := &sshfs.FS{Root: "/var/lib/roundpen", Runner: newMem(), DirMode: 0o777}
	ctx := context.Background()

	info, err := fs.Create(ctx, "ws1", true)
	if err != nil {
		t.Fatal(err)
	}
	if info.HostPath != "/var/lib/roundpen/workspaces/ws1" {
		t.Fatalf("path=%s", info.HostPath)
	}

	if err := fs.Write(ctx, "ws1", "a/b.txt", bytes.NewReader([]byte("hi"))); err != nil {
		t.Fatal(err)
	}
	rc, err := fs.Open(ctx, "ws1", "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "hi" {
		t.Fatalf("got %q", got)
	}

	if _, err := fs.Open(ctx, "ws1", "../etc/passwd"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := fs.Open(ctx, "ws1", "foo/../../etc/passwd"); err == nil {
		t.Fatal("expected escape error")
	}

	if _, err := fs.Get(ctx, "ws1"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove(ctx, "ws1"); err != nil {
		t.Fatal(err)
	}
}

func TestNewRequiresAbsRoot(t *testing.T) {
	if _, err := sshfs.New(config.SSHTarget{User: "u", Host: "h"}, "relative"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewFromDockerHost(t *testing.T) {
	fs, err := sshfs.NewFromDockerHost("ssh://mike@10.10.1.5", "/var/lib/roundpen")
	if err != nil {
		t.Fatal(err)
	}
	if fs.Root != "/var/lib/roundpen" {
		t.Fatalf("root=%s", fs.Root)
	}
}
