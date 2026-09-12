// Package multi routes Create/lifecycle calls to qemu, docker, or kern.
//
// Browser/mobile slots and .qcow2 images always use QEMU. Agent sandboxes follow
// CreateOpts.Engine (or DefaultAgent). OCI images never go to QEMU — they fall
// back to Docker, then Kern.
package multi

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
	"github.com/RoundpenAI/roundpen/internal/backend/kern"
	"github.com/RoundpenAI/roundpen/internal/backend/qemu"
)

// Options configure lazy engine attachment.
type Options struct {
	DataRoot      string
	DockerHost    string
	DockerRuntime string
	DefaultAgent  string // qemu | docker | kern
	DisableQEMU   bool
}

// Backend picks an engine per sandbox based on slot, image, and Engine hint.
type Backend struct {
	DataRoot      string
	DockerHost    string
	DockerRuntime string
	DefaultAgent  string
	disableQEMU   bool

	mu     sync.Mutex
	qemu   *qemu.Backend
	docker *dockerbackend.Backend
	kern   *kern.Backend
	routes map[string]string // sandboxID -> qemu|docker|kern
	qerr   error
	derr   error
}

// New wraps qemu/docker/kern. Engines that are missing on the host are attached
// later when binaries or the daemon appear (no process restart required).
func New(opts Options) *Backend {
	def := strings.ToLower(strings.TrimSpace(opts.DefaultAgent))
	if def == "" {
		def = "qemu"
	}
	return &Backend{
		DataRoot:      opts.DataRoot,
		DockerHost:    opts.DockerHost,
		DockerRuntime: opts.DockerRuntime,
		DefaultAgent:  def,
		disableQEMU:   opts.DisableQEMU,
		kern:          kern.New(),
		routes:        make(map[string]string),
	}
}

// Warm tries to attach QEMU and Docker so existing VMs/containers are recovered.
func (b *Backend) Warm() {
	_, _ = b.qemuEngine()
	_, _ = b.dockerEngine()
}

func (b *Backend) Name() string { return "multi" }

func (b *Backend) qemuEngine() (*qemu.Backend, error) {
	if b.disableQEMU {
		return nil, fmt.Errorf("qemu disabled (ROUNDPEN_QEMU_ENABLED=false)")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.qemu != nil {
		return b.qemu, nil
	}
	qb, err := qemu.New(b.DataRoot)
	if err != nil {
		b.qerr = err
		return nil, err
	}
	b.qemu = qb
	b.qerr = nil
	return qb, nil
}

func (b *Backend) dockerEngine() (*dockerbackend.Backend, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.docker != nil {
		return b.docker, nil
	}
	be, err := dockerbackend.New(b.DockerHost, b.DockerRuntime)
	if err != nil {
		b.derr = err
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := be.Ping(ctx); err != nil {
		_ = be.Close()
		b.derr = err
		return nil, err
	}
	b.docker = be
	b.derr = nil
	return be, nil
}

func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.docker == nil {
		return nil
	}
	err := b.docker.Close()
	b.docker = nil
	return err
}

func (b *Backend) kernEngine() backend.Backend {
	return b.kern
}

func (b *Backend) pickCreate(opts backend.CreateOpts) (backend.Backend, error) {
	slot := strings.ToLower(strings.TrimSpace(opts.Slot))
	if slot == "" {
		slot = strings.ToLower(strings.TrimSpace(opts.Env["ROUNDPEN_SLOT"]))
	}
	switch routeKind(slot, opts.Image, opts.Engine, b.DefaultAgent) {
	case "qemu":
		eng, err := b.qemuEngine()
		if err != nil {
			return nil, fmt.Errorf("qemu: %w", err)
		}
		return eng, nil
	case "docker":
		return b.dockerEngine()
	case "kern":
		return b.kernEngine(), nil
	default: // docker-or-kern: OCI/host when the user asked for QEMU
		if eng, err := b.dockerEngine(); err == nil {
			return eng, nil
		}
		return b.kernEngine(), nil
	}
}

func routeKind(slot, image, engine, defaultAgent string) string {
	slot = strings.ToLower(strings.TrimSpace(slot))
	if slot == "browser" || slot == "mobile" || isQcow2(image) {
		return "qemu"
	}
	want := strings.ToLower(strings.TrimSpace(engine))
	if want == "" {
		want = strings.ToLower(strings.TrimSpace(defaultAgent))
	}
	switch want {
	case "docker":
		return "docker"
	case "kern":
		return "kern"
	case "qemu":
		if isQcow2(image) {
			return "qemu"
		}
		return "docker-or-kern"
	default:
		return "docker-or-kern"
	}
}

func isQcow2(image string) bool {
	return strings.Contains(strings.ToLower(image), ".qcow2")
}

func (b *Backend) remember(id string, eng backend.Backend) {
	if eng == nil || id == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.routes[id] = eng.Name()
}

func (b *Backend) engine(id string) backend.Backend {
	b.mu.Lock()
	route := b.routes[id]
	b.mu.Unlock()
	switch route {
	case "qemu":
		if eng, err := b.qemuEngine(); err == nil {
			return eng
		}
	case "docker":
		if eng, err := b.dockerEngine(); err == nil {
			return eng
		}
	case "kern":
		return b.kernEngine()
	}
	if b.qemu != nil {
		if _, err := b.qemu.VNCSock(id); err == nil {
			b.remember(id, b.qemu)
			return b.qemu
		}
	}
	if qb, err := b.qemuEngine(); err == nil {
		if _, err := qb.VNCSock(id); err == nil {
			b.remember(id, qb)
			return qb
		}
	}
	return b.kernEngine()
}

func (b *Backend) Create(ctx context.Context, opts backend.CreateOpts) (string, error) {
	eng, err := b.pickCreate(opts)
	if err != nil {
		return "", err
	}
	id, err := eng.Create(ctx, opts)
	if err != nil {
		return "", err
	}
	b.remember(opts.SandboxID, eng)
	return id, nil
}

func (b *Backend) Start(ctx context.Context, sandboxID string) error {
	return b.engine(sandboxID).Start(ctx, sandboxID)
}

func (b *Backend) Stop(ctx context.Context, sandboxID string) error {
	return b.engine(sandboxID).Stop(ctx, sandboxID)
}

func (b *Backend) Remove(ctx context.Context, sandboxID string) error {
	eng := b.engine(sandboxID)
	err := eng.Remove(ctx, sandboxID)
	b.mu.Lock()
	delete(b.routes, sandboxID)
	b.mu.Unlock()
	return err
}

func (b *Backend) Exec(ctx context.Context, sandboxID string, opts backend.ExecOpts) (*backend.ExecResult, error) {
	return b.engine(sandboxID).Exec(ctx, sandboxID, opts)
}

func (b *Backend) Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error) {
	return b.engine(sandboxID).Logs(ctx, sandboxID)
}

func (b *Backend) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	return b.engine(sandboxID).Dial(ctx, sandboxID, destPort)
}

func (b *Backend) AttachPTY(ctx context.Context, sandboxID, sessionKey string, opts backend.PTYOpts, stdin io.Reader, stdout io.Writer) error {
	return b.engine(sandboxID).AttachPTY(ctx, sandboxID, sessionKey, opts, stdin, stdout)
}

func (b *Backend) ResizePTY(ctx context.Context, sandboxID, sessionKey string, rows, cols uint16) error {
	return b.engine(sandboxID).ResizePTY(ctx, sandboxID, sessionKey, rows, cols)
}

func (b *Backend) AttachExec(ctx context.Context, sandboxID string, opts backend.AttachExecOpts, stdin io.Reader, stdout, stderr io.Writer) error {
	return b.engine(sandboxID).AttachExec(ctx, sandboxID, opts, stdin, stdout, stderr)
}

// CopyToWorkspace forwards to Docker when the sandbox is on the docker engine.
func (b *Backend) CopyToWorkspace(ctx context.Context, sandboxID, destRel string, r io.Reader) error {
	eng := b.engine(sandboxID)
	c, ok := eng.(interface {
		CopyToWorkspace(context.Context, string, string, io.Reader) error
	})
	if !ok {
		return fmt.Errorf("guest workspace IO requires Docker")
	}
	return c.CopyToWorkspace(ctx, sandboxID, destRel, r)
}

// CopyFromWorkspace forwards to Docker when the sandbox is on the docker engine.
func (b *Backend) CopyFromWorkspace(ctx context.Context, sandboxID, srcRel string) (io.ReadCloser, error) {
	eng := b.engine(sandboxID)
	c, ok := eng.(interface {
		CopyFromWorkspace(context.Context, string, string) (io.ReadCloser, error)
	})
	if !ok {
		return nil, fmt.Errorf("guest workspace IO requires Docker")
	}
	return c.CopyFromWorkspace(ctx, sandboxID, srcRel)
}

// VNCSock delegates to qemu when the sandbox is a VM.
func (b *Backend) VNCSock(sandboxID string) (string, error) {
	eng, err := b.qemuEngine()
	if err != nil {
		return "", err
	}
	return eng.VNCSock(sandboxID)
}

// DockerErr is the last docker attach error (for capability probes).
func (b *Backend) DockerErr() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.derr
}

// QEMUErr is the last qemu attach error (for capability probes).
func (b *Backend) QEMUErr() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.qerr
}

// HasDocker reports whether Docker ping succeeded.
func (b *Backend) HasDocker() bool {
	_, err := b.dockerEngine()
	return err == nil
}
