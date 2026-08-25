// Package kern is the daemonless Backend (Phase 2).
package kern

import (
	"context"
	"fmt"
	"io"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

// Backend is a minimal, daemonless engine peer to Docker.
type Backend struct{}

func New() *Backend { return &Backend{} }

func (b *Backend) Name() string { return "kern" }

func (b *Backend) Create(ctx context.Context, opts backend.CreateOpts) (string, error) {
	return "", fmt.Errorf("kern.Create: not implemented (phase 2)")
}

func (b *Backend) Start(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("kern.Start: not implemented (phase 2)")
}

func (b *Backend) Stop(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("kern.Stop: not implemented (phase 2)")
}

func (b *Backend) Remove(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("kern.Remove: not implemented (phase 2)")
}

func (b *Backend) Exec(ctx context.Context, sandboxID string, opts backend.ExecOpts) (*backend.ExecResult, error) {
	return nil, fmt.Errorf("kern.Exec: not implemented (phase 2)")
}

func (b *Backend) Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("kern.Logs: not implemented (phase 2)")
}

var _ backend.Backend = (*Backend)(nil)
