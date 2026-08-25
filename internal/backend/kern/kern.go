// Package kern is a daemonless Backend: runs commands as host processes
// inside the sandbox workspace directory (no container runtime required).
package kern

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

type status string

const (
	statusCreated status = "created"
	statusRunning status = "running"
	statusStopped status = "stopped"
)

type instance struct {
	id      string
	mount   string
	env     map[string]string
	status  status
	logs    bytes.Buffer
	created time.Time
}

// Backend is a minimal, daemonless engine peer to Docker.
type Backend struct {
	mu   sync.RWMutex
	inst map[string]*instance
}

// New returns a Kern backend.
func New() *Backend {
	return &Backend{inst: make(map[string]*instance)}
}

func (b *Backend) Name() string { return "kern" }

func (b *Backend) Create(ctx context.Context, opts backend.CreateOpts) (string, error) {
	_ = ctx
	if opts.SandboxID == "" {
		return "", fmt.Errorf("sandbox id is required")
	}
	if opts.MountDir == "" {
		return "", fmt.Errorf("mount dir is required")
	}
	abs, err := filepath.Abs(opts.MountDir)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("mount dir %s: %w", abs, err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.inst[opts.SandboxID]; ok {
		return "", fmt.Errorf("sandbox %s already exists", opts.SandboxID)
	}
	env := map[string]string{}
	for k, v := range opts.Env {
		env[k] = v
	}
	// Image is informational for kern (no pull); record for debugging.
	if opts.Image != "" {
		env["ROUNDPEN_IMAGE"] = opts.Image
	}
	inst := &instance{
		id:      opts.SandboxID,
		mount:   abs,
		env:     env,
		status:  statusCreated,
		created: time.Now().UTC(),
	}
	fmt.Fprintf(&inst.logs, "kern created sandbox=%s mount=%s image=%s\n", opts.SandboxID, abs, opts.Image)
	b.inst[opts.SandboxID] = inst
	return "kern:" + opts.SandboxID, nil
}

func (b *Backend) Start(ctx context.Context, sandboxID string) error {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	inst, err := b.getLocked(sandboxID)
	if err != nil {
		return err
	}
	inst.status = statusRunning
	fmt.Fprintf(&inst.logs, "kern started at %s\n", time.Now().UTC().Format(time.RFC3339))
	return nil
}

func (b *Backend) Stop(ctx context.Context, sandboxID string) error {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	inst, err := b.getLocked(sandboxID)
	if err != nil {
		return err
	}
	inst.status = statusStopped
	fmt.Fprintf(&inst.logs, "kern stopped at %s\n", time.Now().UTC().Format(time.RFC3339))
	return nil
}

func (b *Backend) Remove(ctx context.Context, sandboxID string) error {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inst, sandboxID)
	return nil
}

func (b *Backend) Exec(ctx context.Context, sandboxID string, opts backend.ExecOpts) (*backend.ExecResult, error) {
	if len(opts.Cmd) == 0 {
		return nil, fmt.Errorf("cmd is required")
	}

	b.mu.RLock()
	inst, err := b.getLocked(sandboxID)
	if err != nil {
		b.mu.RUnlock()
		return nil, err
	}
	if inst.status != statusRunning {
		b.mu.RUnlock()
		return nil, fmt.Errorf("sandbox %s is %s", sandboxID, inst.status)
	}
	mount := inst.mount
	baseEnv := copyMap(inst.env)
	b.mu.RUnlock()

	workdir, err := resolveWorkDir(mount, opts.WorkDir)
	if err != nil {
		return nil, err
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(execCtx, opts.Cmd[0], opts.Cmd[1:]...)
	cmd.Dir = workdir
	cmd.Env = buildEnv(mount, workdir, baseEnv, opts.Env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
			runErr = nil
		} else if execCtx.Err() != nil {
			return nil, fmt.Errorf("exec timeout/cancel: %w", execCtx.Err())
		} else {
			return nil, fmt.Errorf("exec: %w", runErr)
		}
	}

	b.mu.Lock()
	if inst, ok := b.inst[sandboxID]; ok {
		fmt.Fprintf(&inst.logs, "exec %v exit=%d\n", opts.Cmd, exitCode)
	}
	b.mu.Unlock()

	return &backend.ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
	}, nil
}

func (b *Backend) Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error) {
	_ = ctx
	b.mu.RLock()
	defer b.mu.RUnlock()
	inst, err := b.getLocked(sandboxID)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(inst.logs.Bytes())), nil
}

func (b *Backend) getLocked(id string) (*instance, error) {
	inst, ok := b.inst[id]
	if !ok {
		return nil, fmt.Errorf("sandbox %s not found", id)
	}
	return inst, nil
}

func resolveWorkDir(mount, workDir string) (string, error) {
	if workDir == "" || workDir == "/workspace" {
		return mount, nil
	}
	var candidate string
	if strings.HasPrefix(workDir, "/workspace/") {
		candidate = filepath.Join(mount, strings.TrimPrefix(workDir, "/workspace/"))
	} else if filepath.IsAbs(workDir) {
		candidate = workDir
	} else {
		candidate = filepath.Join(mount, workDir)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	mountAbs, err := filepath.Abs(mount)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(mountAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("workdir escapes workspace")
	}
	return abs, nil
}

func buildEnv(mount, workdir string, base, extra map[string]string) []string {
	merged := map[string]string{
		"PATH":  "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME":  mount,
		"PWD":   workdir,
		"SHELL": "/bin/sh",
	}
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var _ backend.Backend = (*Backend)(nil)
