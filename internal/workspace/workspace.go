// Package workspace defines WorkspaceFS: file content on disk, metadata in Postgres.
package workspace

import (
	"context"
	"io"
	"os"
)

// Info describes a workspace directory on the host.
type Info struct {
	ID        string
	HostPath  string // absolute path under data_root
	Ephemeral bool
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
}
