// Package local implements WorkspaceFS on the local filesystem.
package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// FS stores workspaces under Root/workspaces/{id}.
type FS struct {
	Root string
}

// New returns a local FS rooted at root (typically config.DataRoot).
func New(root string) *FS {
	return &FS{Root: root}
}

func (f *FS) base(id string) string {
	return filepath.Join(f.Root, "workspaces", id)
}

func (f *FS) Create(ctx context.Context, id string, ephemeral bool) (*workspace.Info, error) {
	_ = ctx
	path := f.base(id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	return &workspace.Info{ID: id, HostPath: path, Ephemeral: ephemeral}, nil
}

func (f *FS) Get(ctx context.Context, id string) (*workspace.Info, error) {
	_ = ctx
	path := f.base(id)
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("workspace %s: not a directory", id)
	}
	return &workspace.Info{ID: id, HostPath: path}, nil
}

func (f *FS) Remove(ctx context.Context, id string) error {
	_ = ctx
	return os.RemoveAll(f.base(id))
}

func (f *FS) resolve(id, relPath string) (string, error) {
	base := f.base(id)
	full := filepath.Join(base, filepath.Clean("/"+relPath))
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || len(rel) >= 2 && rel[:2] == ".." {
		return "", fmt.Errorf("path escapes workspace")
	}
	return full, nil
}

func (f *FS) Open(ctx context.Context, id, relPath string) (io.ReadCloser, error) {
	_ = ctx
	p, err := f.resolve(id, relPath)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (f *FS) Write(ctx context.Context, id, relPath string, r io.Reader) error {
	_ = ctx
	p, err := f.resolve(id, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	file, err := os.Create(p)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	return err
}

func (f *FS) Stat(ctx context.Context, id, relPath string) (os.FileInfo, error) {
	_ = ctx
	p, err := f.resolve(id, relPath)
	if err != nil {
		return nil, err
	}
	return os.Stat(p)
}

var _ workspace.FS = (*FS)(nil)
