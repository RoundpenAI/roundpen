// Package sandbox defines the domain model and manager for agent sandboxes.
// It depends on backend and workspace abstractions, not on HTTP or Docker details.
package sandbox

import (
	"context"
	"time"
)

// Status is the lifecycle state of a sandbox.
type Status string

const (
	StatusCreating Status = "creating"
	StatusRunning  Status = "running"
	StatusPaused   Status = "paused"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

// Sandbox is the control-plane view of an isolated execution environment.
type Sandbox struct {
	ID          string
	Status      Status
	Image       string
	WorkspaceID string
	CreatedAt   time.Time
	Metadata    map[string]string
}

// CreateRequest is the input to create a sandbox.
type CreateRequest struct {
	Image       string
	WorkspaceID string // empty => ephemeral workspace
	Metadata    map[string]string
}

// ExecRequest runs a command inside a sandbox.
type ExecRequest struct {
	Cmd     []string
	WorkDir string
	Env     map[string]string
	Timeout time.Duration
}

// ExecResult is the outcome of an exec.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

// Manager orchestrates sandbox lifecycle via a Backend and WorkspaceFS.
type Manager interface {
	Create(ctx context.Context, req CreateRequest) (*Sandbox, error)
	Get(ctx context.Context, id string) (*Sandbox, error)
	List(ctx context.Context) ([]*Sandbox, error)
	Stop(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	Exec(ctx context.Context, id string, req ExecRequest) (*ExecResult, error)
}
