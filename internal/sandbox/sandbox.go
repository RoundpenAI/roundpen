// Package sandbox defines the domain model and manager for agent sandboxes.
// It depends on backend and workspace abstractions, not on HTTP or Docker details.
package sandbox

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/RoundpenAI/roundpen/internal/workspace"
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

// ErrConflict is returned when a unique name or category-default constraint fails.
var ErrConflict = errors.New("conflict")

// ErrNotFound is returned when a sandbox does not exist.
var ErrNotFound = errors.New("not found")

// Sandbox is the control-plane view of an isolated execution environment.
type Sandbox struct {
	ID            string
	Name          string // human-readable label; unique (case-insensitive)
	Category      string // free-form class, e.g. Browser / Code
	IsDefault     bool   // default sandbox within Category
	ContainerID   string
	Status        Status
	Image         string
	WorkspaceID   string
	WorkspacePath string
	TTLSeconds    int
	ExpiresAt     *time.Time
	LastActiveAt  time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Metadata      map[string]string
	CPUCount      int
	MemoryMB      int
	DiskSizeMB    int
	TemplateBuild string
}

// CreateRequest is the input to create a sandbox.
type CreateRequest struct {
	Name        string // optional display name; defaulted if empty
	Category    string // optional class for resolve-by-category
	IsDefault   bool   // mark as default within Category
	Image       string
	TemplateID  string // E2B-style alias; maps to Image when Image empty
	WorkspaceID string // empty => ephemeral workspace = sandbox id
	TTL         time.Duration
	Env         map[string]string
	Metadata    map[string]string
}

// UpdateRequest patches mutable labels / classification.
type UpdateRequest struct {
	Name      *string
	Category  *string
	IsDefault *bool
}

// ResolveRequest looks up a sandbox by name or by category default.
// Prefer Name when both are set.
type ResolveRequest struct {
	Name     string
	Category string
}

// ListFilter narrows List results. Empty fields are ignored.
type ListFilter struct {
	Category string
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

// TerminalOpts configures an interactive terminal.
type TerminalOpts struct {
	Cmd     []string
	WorkDir string
	Env     []string
	Rows    uint16
	Cols    uint16
}

// Store persists sandbox records.
type Store interface {
	Insert(ctx context.Context, sb *Sandbox) error
	Update(ctx context.Context, sb *Sandbox) error
	SoftDelete(ctx context.Context, id string, at time.Time) error
	Get(ctx context.Context, id string) (*Sandbox, error)
	GetByName(ctx context.Context, name string) (*Sandbox, error)
	GetDefaultByCategory(ctx context.Context, category string) (*Sandbox, error)
	List(ctx context.Context) ([]*Sandbox, error)
	ListByCategory(ctx context.Context, category string) ([]*Sandbox, error)
	ClearDefaultInCategory(ctx context.Context, category, exceptID string) error
}

// Manager orchestrates sandbox lifecycle via a Backend and WorkspaceFS.
type Manager interface {
	Create(ctx context.Context, req CreateRequest) (*Sandbox, error)
	Get(ctx context.Context, id string) (*Sandbox, error)
	List(ctx context.Context, filter ListFilter) ([]*Sandbox, error)
	Resolve(ctx context.Context, req ResolveRequest) (*Sandbox, error)
	Stop(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	SetTimeout(ctx context.Context, id string, ttl time.Duration) (*Sandbox, error)
	Connect(ctx context.Context, id string) (*Sandbox, bool, error)
	Refresh(ctx context.Context, id string) (*Sandbox, error)
	Rename(ctx context.Context, id, name string) (*Sandbox, error)
	Update(ctx context.Context, id string, req UpdateRequest) (*Sandbox, error)
	Exec(ctx context.Context, id string, req ExecRequest) (*ExecResult, error)

	ListFiles(ctx context.Context, id, relPath string) ([]workspace.DirEntry, error)
	StatFile(ctx context.Context, id, relPath string) (*workspace.FileStat, error)
	ReadFile(ctx context.Context, id, relPath string) (io.ReadCloser, error)
	WriteFile(ctx context.Context, id, relPath string, r io.Reader) error
	RemoveFile(ctx context.Context, id, relPath string) error
	WorkspaceHostPath(ctx context.Context, id string) (string, error)

	Dial(ctx context.Context, id string, port int) (net.Conn, error)
	Touch(ctx context.Context, id string) error

	AttachTerminal(ctx context.Context, id, sessionKey string, opts TerminalOpts, stdin io.Reader, stdout io.Writer) error
	ResizeTerminal(ctx context.Context, id, sessionKey string, rows, cols uint16) error
}
