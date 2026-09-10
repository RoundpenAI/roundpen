// Package kern is a daemonless Backend: runs commands in a bubblewrap jail
// with guest paths /workspace and /home (no container runtime required).
package kern

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"

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
	name    string
	mount   string // host path for guest /workspace
	home    string // host path for guest /home
	env     map[string]string
	status  status
	logs    bytes.Buffer
	created time.Time
}

// Backend is a minimal, daemonless engine peer to Docker.
type Backend struct {
	mu   sync.RWMutex
	inst map[string]*instance

	ptyMu sync.Mutex
	ptys  map[string]*os.File // sessionKey -> pty master
}

// New returns a Kern backend.
func New() *Backend {
	return &Backend{
		inst: make(map[string]*instance),
		ptys: make(map[string]*os.File),
	}
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
	home, err := ensureSandboxDirs(abs)
	if err != nil {
		return "", err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	env := map[string]string{}
	for k, v := range opts.Env {
		env[k] = v
	}
	if opts.Image != "" {
		env["ROUNDPEN_IMAGE"] = opts.Image
	}
	// Idempotent: survive roundpend restarts while the DB still says "running".
	if inst, ok := b.inst[opts.SandboxID]; ok {
		inst.mount = abs
		inst.home = home
		inst.name = opts.Name
		mergeEnv(inst, env)
		return "kern:" + opts.SandboxID, nil
	}
	inst := &instance{
		id:      opts.SandboxID,
		name:    opts.Name,
		mount:   abs,
		home:    home,
		env:     env,
		status:  statusCreated,
		created: time.Now().UTC(),
	}
	fmt.Fprintf(&inst.logs, "kern created sandbox=%s workspace=%s home=%s image=%s\n", opts.SandboxID, abs, home, opts.Image)
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
	home := inst.home
	host := guestHostname(sandboxID, inst.name)
	baseEnv := copyMap(inst.env)
	b.mu.RUnlock()

	hostWork, err := resolveWorkDir(mount, opts.WorkDir)
	if err != nil {
		return nil, err
	}
	guestPWD, err := guestWorkDir(mount, hostWork)
	if err != nil {
		return nil, err
	}
	if _, err := ensureSandboxDirs(mount); err != nil {
		return nil, err
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	env := buildJailEnv(baseEnv, opts.Env, guestPWD, host)
	path, args, err := wrapBwrap(host, mount, home, guestPWD, opts.Cmd, env)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(execCtx, path, args...)

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

func (b *Backend) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	if destPort <= 0 || destPort > 65535 {
		return nil, fmt.Errorf("invalid port %d", destPort)
	}
	b.mu.RLock()
	inst, err := b.getLocked(sandboxID)
	b.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	if inst.status != statusRunning {
		return nil, fmt.Errorf("sandbox %s is %s", sandboxID, inst.status)
	}
	return nil, fmt.Errorf("preview is not supported on the kern backend")
}

func (b *Backend) AttachPTY(ctx context.Context, sandboxID, sessionKey string, opts backend.PTYOpts, stdin io.Reader, stdout io.Writer) error {
	b.mu.RLock()
	inst, err := b.getLocked(sandboxID)
	if err != nil {
		b.mu.RUnlock()
		return err
	}
	if inst.status != statusRunning {
		b.mu.RUnlock()
		return fmt.Errorf("sandbox %s is %s", sandboxID, inst.status)
	}
	mount := inst.mount
	home := inst.home
	host := guestHostname(sandboxID, inst.name)
	baseEnv := copyMap(inst.env)
	b.mu.RUnlock()

	cmdArgs := opts.Cmd
	if len(cmdArgs) == 0 {
		shell := "/bin/bash"
		if _, err := os.Stat(shell); err != nil {
			shell = "/bin/sh"
		}
		cmdArgs = []string{shell}
	}
	hostWork, err := resolveWorkDir(mount, opts.WorkDir)
	if err != nil {
		return err
	}
	guestPWD, err := guestWorkDir(mount, hostWork)
	if err != nil {
		return err
	}
	if _, err := ensureSandboxDirs(mount); err != nil {
		return err
	}
	rows, cols := opts.Rows, opts.Cols
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}

	env := buildJailEnv(baseEnv, nil, guestPWD, host)
	for _, e := range opts.Env {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			env = append(env, k+"="+v)
		}
	}
	path, args, err := wrapBwrap(host, mount, home, guestPWD, cmdArgs, env)
	if err != nil {
		return err
	}
	c := exec.CommandContext(ctx, path, args...)

	ptmx, err := pty.Start(c)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})

	if sessionKey != "" {
		b.ptyMu.Lock()
		b.ptys[sessionKey] = ptmx
		b.ptyMu.Unlock()
		defer func() {
			b.ptyMu.Lock()
			delete(b.ptys, sessionKey)
			b.ptyMu.Unlock()
		}()
	}

	errCh := make(chan error, 2)
	go func() {
		_, copyErr := io.Copy(ptmx, stdin)
		errCh <- copyErr
	}()
	go func() {
		_, copyErr := io.Copy(stdout, ptmx)
		errCh <- copyErr
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- c.Wait() }()

	select {
	case <-ctx.Done():
		_ = ptmx.Close()
		_ = c.Process.Kill()
		return ctx.Err()
	case err := <-waitCh:
		_ = ptmx.Close()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				_ = ee
				return nil
			}
			return err
		}
		return nil
	case err := <-errCh:
		_ = ptmx.Close()
		_ = c.Process.Kill()
		if err != nil && err != io.EOF && ctx.Err() == nil {
			return err
		}
		return nil
	}
}

func (b *Backend) ResizePTY(ctx context.Context, sandboxID, sessionKey string, rows, cols uint16) error {
	_ = ctx
	_ = sandboxID
	b.ptyMu.Lock()
	ptmx := b.ptys[sessionKey]
	b.ptyMu.Unlock()
	if ptmx == nil {
		return fmt.Errorf("pty session %q not found", sessionKey)
	}
	return pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})
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
		// Absolute host paths are rejected — only guest /workspace paths are allowed.
		return "", fmt.Errorf("workdir must be under /workspace")
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

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// mergeEnv overlays new keys onto inst.env without clearing vars when hydrate
// re-attaches with an empty env map (CreateRequest env is not persisted yet).
func mergeEnv(inst *instance, env map[string]string) {
	if len(env) == 0 {
		return
	}
	if inst.env == nil {
		inst.env = make(map[string]string, len(env))
	}
	for k, v := range env {
		inst.env[k] = v
	}
}

var _ backend.Backend = (*Backend)(nil)
