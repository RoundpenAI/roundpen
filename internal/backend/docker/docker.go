// Package docker implements the Docker Daemon Backend (Phase 1 default).
package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

// Backend talks to a local or remote Docker Engine.
// Concrete client wiring is intentionally deferred until Phase 1 implementation.
type Backend struct {
	Host string // e.g. unix:///var/run/docker.sock
}

// New returns a Docker backend. host may be empty for the default socket.
func New(host string) *Backend {
	return &Backend{Host: host}
}

func (b *Backend) Name() string { return "docker" }

func (b *Backend) Create(ctx context.Context, opts backend.CreateOpts) error {
	return fmt.Errorf("docker.Create: not implemented")
}

func (b *Backend) Start(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("docker.Start: not implemented")
}

func (b *Backend) Stop(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("docker.Stop: not implemented")
}

func (b *Backend) Remove(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("docker.Remove: not implemented")
}

func (b *Backend) Exec(ctx context.Context, sandboxID string, opts backend.ExecOpts) (*backend.ExecResult, error) {
	return nil, fmt.Errorf("docker.Exec: not implemented")
}

func (b *Backend) Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("docker.Logs: not implemented")
}

var _ backend.Backend = (*Backend)(nil)
