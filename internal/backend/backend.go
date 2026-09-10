// Package backend defines pluggable sandbox engines (Docker, Kern, K8s, …).
package backend

import (
	"context"
	"io"
	"net"
	"time"
)

// CreateOpts configures engine-level sandbox creation.
type CreateOpts struct {
	SandboxID   string
	Name        string // optional display name (kern jail hostname)
	Image       string
	MountDir    string // host path mounted at /workspace
	Env         map[string]string
	MemoryLimit int64 // bytes; 0 = default
	CPULimit    float64
	UseImageCmd bool   // keep image ENTRYPOINT/CMD (template snapshots)
	Slot        string // agent | browser | mobile — selects engine when using a multi backend
	Engine      string // qemu | docker | kern — user/slot preference for agent
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

// PTYOpts configures an interactive terminal session.
type PTYOpts struct {
	Cmd     []string // default ["/bin/bash"] or ["/bin/sh"] (PTY ⇒ interactive)
	WorkDir string   // guest /workspace or relative; default /workspace
	Env     []string
	Rows    uint16
	Cols    uint16
}

// AttachExecOpts configures a long-lived non-TTY process (e.g. ACP agent stdio).
type AttachExecOpts struct {
	Cmd     []string
	WorkDir string
	Env     map[string]string
}

// Backend is the infrastructure engine behind Sandbox Manager.
// OCI runtimes (runc/crun/gVisor/Kata) are options of a Backend, not sibling Backends.
// Methods take the Roundpen sandbox id (not the engine-local container id).
type Backend interface {
	Name() string
	// Create provisions the engine sandbox and returns an engine-local id (e.g. container id).
	Create(ctx context.Context, opts CreateOpts) (engineID string, err error)
	Start(ctx context.Context, sandboxID string) error
	Stop(ctx context.Context, sandboxID string) error
	Remove(ctx context.Context, sandboxID string) error
	Exec(ctx context.Context, sandboxID string, opts ExecOpts) (*ExecResult, error)
	Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error)

	// Dial opens a TCP connection to destPort inside the sandbox network.
	Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)

	// AttachPTY runs an interactive PTY until the session ends or ctx is cancelled.
	// sessionKey identifies the session for ResizePTY while AttachPTY is in progress.
	AttachPTY(ctx context.Context, sandboxID, sessionKey string, opts PTYOpts, stdin io.Reader, stdout io.Writer) error
	ResizePTY(ctx context.Context, sandboxID, sessionKey string, rows, cols uint16) error

	// AttachExec runs a non-TTY command with streamed stdin/stdout/stderr until exit or ctx cancel.
	// stdout and stderr may be the same Writer. Used for ACP stdio bridging.
	AttachExec(ctx context.Context, sandboxID string, opts AttachExecOpts, stdin io.Reader, stdout, stderr io.Writer) error
}
