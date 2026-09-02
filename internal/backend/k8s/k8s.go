// Package k8s is the Kubernetes Backend (Phase 4 placeholder).
package k8s

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

// Backend schedules sandboxes onto a cluster. Not an Operator in MVP roadmap.
type Backend struct{}

func New() *Backend { return &Backend{} }

func (b *Backend) Name() string { return "k8s" }

func (b *Backend) Create(ctx context.Context, opts backend.CreateOpts) (string, error) {
	return "", fmt.Errorf("k8s.Create: not implemented (phase 4)")
}

func (b *Backend) Start(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("k8s.Start: not implemented (phase 4)")
}

func (b *Backend) Stop(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("k8s.Stop: not implemented (phase 4)")
}

func (b *Backend) Remove(ctx context.Context, sandboxID string) error {
	return fmt.Errorf("k8s.Remove: not implemented (phase 4)")
}

func (b *Backend) Exec(ctx context.Context, sandboxID string, opts backend.ExecOpts) (*backend.ExecResult, error) {
	return nil, fmt.Errorf("k8s.Exec: not implemented (phase 4)")
}

func (b *Backend) Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("k8s.Logs: not implemented (phase 4)")
}

func (b *Backend) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	return nil, fmt.Errorf("k8s.Dial: not implemented (phase 4)")
}

func (b *Backend) AttachPTY(ctx context.Context, sandboxID, sessionKey string, opts backend.PTYOpts, stdin io.Reader, stdout io.Writer) error {
	return fmt.Errorf("k8s.AttachPTY: not implemented (phase 4)")
}

func (b *Backend) ResizePTY(ctx context.Context, sandboxID, sessionKey string, rows, cols uint16) error {
	return fmt.Errorf("k8s.ResizePTY: not implemented (phase 4)")
}

var _ backend.Backend = (*Backend)(nil)
