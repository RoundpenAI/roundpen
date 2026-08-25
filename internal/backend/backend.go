// Package backend defines pluggable sandbox engines (Docker, Kern, K8s, …).
package backend

import (
	"context"
	"io"
	"time"
)

// CreateOpts configures engine-level sandbox creation.
type CreateOpts struct {
	SandboxID   string
	Image       string
	MountDir    string // host path mounted at /workspace
	Env         map[string]string
	MemoryLimit int64 // bytes; 0 = default
	CPULimit    float64
}

// ExecOpts configures a command run inside the engine sandbox.
type ExecOpts struct {
	Cmd     []string
	WorkDir string
	Env     map[string]string
	Timeout time.Duration
}

// ExecResult is engine-level command output.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

// Backend is the infrastructure engine behind Sandbox Manager.
// OCI runtimes (runc/crun/gVisor/Kata) are options of a Backend, not sibling Backends.
type Backend interface {
	Name() string
	Create(ctx context.Context, opts CreateOpts) error
	Start(ctx context.Context, sandboxID string) error
	Stop(ctx context.Context, sandboxID string) error
	Remove(ctx context.Context, sandboxID string) error
	Exec(ctx context.Context, sandboxID string, opts ExecOpts) (*ExecResult, error)
	Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error)
}
