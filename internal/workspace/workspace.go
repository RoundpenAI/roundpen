// Package workspace defines WorkspaceFS: file content on disk, metadata in Postgres.
package workspace

import (
	"context"
	"io"
	"os"
	"time"
)

// Info describes a workspace directory on the host.
type Info struct {
	ID        string
	HostPath  string // absolute host path for guest /workspace
	HomePath  string // absolute host path for guest /home (parallel to workspace)
	Ephemeral bool
}

// DirEntry is one directory listing row.
type DirEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time,omitempty"`
}

// FileStat describes one path inside a workspace.
type FileStat struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time,omitempty"`
}

// FS abstracts workspace directory lifecycle and basic file IO on the host side.
// Sandbox mounts HostPath at /workspace via the Backend.
type FS interface {
	Create(ctx context.Context, id string, ephemeral bool) (*Info, error)
	Get(ctx context.Context, id string) (*Info, error)
	Remove(ctx context.Context, id string) error
	Open(ctx context.Context, id, relPath string) (io.ReadCloser, error)
	Write(ctx context.Context, id, relPath string, r io.Reader) error
	Stat(ctx context.Context, id, relPath string) (os.FileInfo, error)
	List(ctx context.Context, id, relPath string) ([]DirEntry, error)
	RemovePath(ctx context.Context, id, relPath string) error
}
