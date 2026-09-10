// Package local implements WorkspaceFS on the local filesystem.
package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// FS stores each sandbox under Root/sandboxes/{id}/ with sibling dirs:
//
//	workspace/  → guest /workspace (Files API)
//	home/       → guest /home (HOME)
//
// Legacy Root/workspaces/{id} is still readable for older records.
type FS struct {
	Root string
}

// New returns a local FS rooted at root (typically config.DataRoot).
// Root is cleaned to an absolute path so listings do not depend on process cwd.
func New(root string) *FS {
	abs := root
	if root != "" {
		if a, err := filepath.Abs(root); err == nil {
			abs = a
		}
	}
	return &FS{Root: abs}
}

func (f *FS) sandboxDir(id string) string {
	return filepath.Join(f.Root, "sandboxes", id)
}

func (f *FS) workspacePath(id string) string {
	return filepath.Join(f.sandboxDir(id), "workspace")
}

func (f *FS) homePath(id string) string {
	return filepath.Join(f.sandboxDir(id), "home")
}

func (f *FS) legacyWorkspacePath(id string) string {
	return filepath.Join(f.Root, "workspaces", id)
}

// base returns the host directory that backs guest /workspace.
func (f *FS) base(id string) string {
	p := f.workspacePath(id)
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		return p
	}
	legacy := f.legacyWorkspacePath(id)
	if fi, err := os.Stat(legacy); err == nil && fi.IsDir() {
		return legacy
	}
	return p
}

func (f *FS) Create(ctx context.Context, id string, ephemeral bool) (*workspace.Info, error) {
	_ = ctx
	ws := f.workspacePath(id)
	home := f.homePath(id)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	return &workspace.Info{ID: id, HostPath: ws, HomePath: home, Ephemeral: ephemeral}, nil
}

func (f *FS) Get(ctx context.Context, id string) (*workspace.Info, error) {
	_ = ctx
	ws := f.base(id)
	fi, err := os.Stat(ws)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("workspace %s: not a directory", id)
	}
	home := f.homePath(id)
	if filepath.Dir(ws) == f.sandboxDir(id) {
		_ = os.MkdirAll(home, 0o755)
	} else {
		// Legacy workspaces/{id}: keep home as sibling next to workspace dir.
		home = filepath.Join(filepath.Dir(ws), id+".home")
		_ = os.MkdirAll(home, 0o755)
	}
	return &workspace.Info{ID: id, HostPath: ws, HomePath: home}, nil
}

func (f *FS) Remove(ctx context.Context, id string) error {
	_ = ctx
	_ = os.RemoveAll(f.sandboxDir(id))
	_ = os.RemoveAll(f.legacyWorkspacePath(id))
	_ = os.RemoveAll(filepath.Join(f.Root, "workspaces", id+".home"))
	return nil
}

func (f *FS) resolve(id, relPath string) (string, error) {
	base := f.base(id)
	cleaned := filepath.Clean(strings.TrimSpace(relPath))
	if cleaned == "." || cleaned == "" || cleaned == string(filepath.Separator) {
		return confineExisting(base, base)
	}
	// Never pass an absolute cleaned path to Join — it would discard base.
	cleaned = strings.TrimPrefix(cleaned, string(filepath.Separator))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	full := filepath.Join(base, cleaned)
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return confinePath(base, full)
}

func stayUnder(base, path string) error {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes workspace")
	}
	return nil
}

func confineExisting(base, path string) (string, error) {
	if err := stayUnder(base, path); err != nil {
		return "", err
	}
	return path, nil
}

func followIfSafe(base, path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("path escapes workspace")
	}
	if err := stayUnder(base, target); err != nil {
		return "", err
	}
	return target, nil
}

// confinePath walks parent directories with Lstat. Intermediate symlinks may
// be followed only when the target stays under base. The final component is
// returned as-is so callers can unlink a dangling or outbound symlink.
func confinePath(base, full string) (string, error) {
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	if rel == "." {
		return confineExisting(base, base)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := base
	for i, part := range parts {
		if part == "." || part == "" {
			continue
		}
		next := filepath.Join(current, part)
		last := i == len(parts)-1
		fi, err := os.Lstat(next)
		if os.IsNotExist(err) {
			return full, nil
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if last {
				return next, nil
			}
			target, err := filepath.EvalSymlinks(next)
			if err != nil {
				return "", fmt.Errorf("path escapes workspace")
			}
			if err := stayUnder(base, target); err != nil {
				return "", err
			}
			current = target
			continue
		}
		current = next
	}
	return current, nil
}

func (f *FS) Open(ctx context.Context, id, relPath string) (io.ReadCloser, error) {
	_ = ctx
	p, err := f.resolve(id, relPath)
	if err != nil {
		return nil, err
	}
	p, err = followIfSafe(f.base(id), p)
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
	if _, err := os.Lstat(p); err == nil {
		if _, err := followIfSafe(f.base(id), p); err != nil {
			return err
		}
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
	p, err = followIfSafe(f.base(id), p)
	if err != nil {
		return nil, err
	}
	return os.Stat(p)
}

func (f *FS) List(ctx context.Context, id, relPath string) ([]workspace.DirEntry, error) {
	_ = ctx
	p, err := f.resolve(id, relPath)
	if err != nil {
		return nil, err
	}
	p, err = followIfSafe(f.base(id), p)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]workspace.DirEntry, 0, len(entries))
	for _, e := range entries {
		de := workspace.DirEntry{Name: e.Name(), IsDir: e.IsDir()}
		if fi, err := e.Info(); err == nil {
			de.Size = fi.Size()
			de.ModTime = fi.ModTime().UTC()
		}
		out = append(out, de)
	}
	return out, nil
}

func (f *FS) RemovePath(ctx context.Context, id, relPath string) error {
	_ = ctx
	if relPath == "" || relPath == "." || relPath == "/" {
		return fmt.Errorf("refusing to remove workspace root; use Remove")
	}
	p, err := f.resolve(id, relPath)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return os.Remove(p)
	}
	return os.RemoveAll(p)
}

var _ workspace.FS = (*FS)(nil)
