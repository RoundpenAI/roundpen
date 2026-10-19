package local

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Tree reads and writes one absolute workspace directory outside the data root.
type Tree struct {
	Base string
}

func (t Tree) Open(relPath string) (io.ReadCloser, error) {
	p, err := resolveUnder(t.Base, relPath)
	if err != nil {
		return nil, err
	}
	p, err = followIfSafe(t.Base, p)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (t Tree) Write(relPath string, r io.Reader) error {
	p, err := resolveUnder(t.Base, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(p); err == nil {
		if _, err := followIfSafe(t.Base, p); err != nil {
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

func (t Tree) Stat(relPath string) (os.FileInfo, error) {
	p, err := resolveUnder(t.Base, relPath)
	if err != nil {
		return nil, err
	}
	p, err = followIfSafe(t.Base, p)
	if err != nil {
		return nil, err
	}
	return os.Stat(p)
}

func (t Tree) ReadDir(relPath string) ([]os.DirEntry, error) {
	p, err := resolveUnder(t.Base, relPath)
	if err != nil {
		return nil, err
	}
	p, err = followIfSafe(t.Base, p)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(p)
}

func (t Tree) RemovePath(relPath string) error {
	if relPath == "" || relPath == "." || relPath == "/" {
		return fmt.Errorf("refusing to remove workspace root")
	}
	p, err := resolveUnder(t.Base, relPath)
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

func resolveUnder(base, relPath string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(relPath))
	if cleaned == "." || cleaned == "" || cleaned == string(filepath.Separator) {
		return confineExisting(base, base)
	}
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
