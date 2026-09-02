package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/config"
)

// Backend talks to a local or remote Docker Engine.
// Remote hosts should use ssh://user@host (requires SSH key auth and a remote docker CLI).
type Backend struct {
	cli     *client.Client
	runtime string
	host    string // original DOCKER_HOST (for Dial over ssh://)

	ptyMu sync.Mutex
	ptys  map[string]string // sessionKey -> exec ID
}

// New connects to Docker. host may be empty for the SDK default.
// Supported examples: unix:///var/run/docker.sock, ssh://user@host.
func New(host, runtime string) (*Backend, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if host != "" {
		helper, err := connhelper.GetConnectionHelper(host)
		if err != nil {
			return nil, fmt.Errorf("docker host %q: %w", host, err)
		}
		if helper != nil {
			httpClient := &http.Client{
				Transport: &http.Transport{
					DialContext: helper.Dialer,
				},
			}
			opts = append(opts,
				client.WithHTTPClient(httpClient),
				client.WithHost(helper.Host),
				client.WithDialContext(helper.Dialer),
			)
		} else {
			opts = append(opts, client.WithHost(host))
		}
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Backend{cli: cli, runtime: runtime, host: host, ptys: make(map[string]string)}, nil
}

// Close releases the Docker client.
func (b *Backend) Close() error {
	if b.cli == nil {
		return nil
	}
	return b.cli.Close()
}

func (b *Backend) Name() string { return "docker" }

func containerName(sandboxID string) string {
	return "roundpen-" + sandboxID
}

func (b *Backend) Create(ctx context.Context, opts backend.CreateOpts) (string, error) {
	if opts.Image == "" {
		return "", fmt.Errorf("image is required")
	}
	if err := b.ensureImage(ctx, opts.Image); err != nil {
		return "", err
	}

	env := make([]string, 0, len(opts.Env))
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}

	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{},
		Resources: container.Resources{
			Memory: opts.MemoryLimit,
		},
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
	}
	if opts.MountDir != "" {
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: opts.MountDir,
			Target: "/workspace",
		})
	}
	if b.runtime != "" {
		hostCfg.Runtime = b.runtime
	}
	if opts.CPULimit > 0 {
		hostCfg.NanoCPUs = int64(opts.CPULimit * 1e9)
	}

	cfg := &container.Config{
		Image:      opts.Image,
		Env:        env,
		WorkingDir: "/workspace",
		Labels: map[string]string{
			"roundpen.sandbox_id": opts.SandboxID,
		},
	}
	if opts.UseImageCmd {
		// Keep image ENTRYPOINT/CMD (template snapshot with init script).
	} else {
		cfg.Cmd = []string{"sleep", "infinity"}
	}

	name := containerName(opts.SandboxID)
	resp, err := b.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	if err != nil {
		return "", fmt.Errorf("container create: %w", err)
	}
	return resp.ID, nil
}

func (b *Backend) ensureImage(ctx context.Context, ref string) error {
	_, _, err := b.cli.ImageInspectWithRaw(ctx, ref)
	if err == nil {
		return nil
	}
	rc, err := b.cli.ImagePull(ctx, ref, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("image pull %s: %w", ref, err)
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc)
	return nil
}

func (b *Backend) Start(ctx context.Context, sandboxID string) error {
	return b.cli.ContainerStart(ctx, containerName(sandboxID), container.StartOptions{})
}

func (b *Backend) Stop(ctx context.Context, sandboxID string) error {
	timeout := 10
	return b.cli.ContainerStop(ctx, containerName(sandboxID), container.StopOptions{Timeout: &timeout})
}

func (b *Backend) Remove(ctx context.Context, sandboxID string) error {
	name := containerName(sandboxID)
	_ = b.cli.ContainerStop(ctx, name, container.StopOptions{})
	err := b.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && client.IsErrNotFound(err) {
		return nil
	}
	return err
}

func (b *Backend) Exec(ctx context.Context, sandboxID string, opts backend.ExecOpts) (*backend.ExecResult, error) {
	cmd := opts.Cmd
	if len(cmd) == 0 {
		return nil, fmt.Errorf("cmd is required")
	}
	workdir := opts.WorkDir
	if workdir == "" {
		workdir = "/workspace"
	}
	env := make([]string, 0, len(opts.Env))
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	createResp, err := b.cli.ContainerExecCreate(execCtx, containerName(sandboxID), types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Env:          env,
		WorkingDir:   workdir,
		Cmd:          cmd,
	})
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}

	attach, err := b.cli.ContainerExecAttach(execCtx, createResp.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}
	defer attach.Close()

	var stdout, stderr strings.Builder
	_, err = stdcopy.StdCopy(&stdout, &stderr, attach.Reader)
	if err != nil && execCtx.Err() == nil {
		return nil, fmt.Errorf("exec copy: %w", err)
	}

	inspect, err := b.cli.ContainerExecInspect(ctx, createResp.ID)
	if err != nil {
		return nil, fmt.Errorf("exec inspect: %w", err)
	}
	return &backend.ExecResult{
		ExitCode: inspect.ExitCode,
		Stdout:   []byte(stdout.String()),
		Stderr:   []byte(stderr.String()),
	}, nil
}

func (b *Backend) Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error) {
	return b.cli.ContainerLogs(ctx, containerName(sandboxID), container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "200",
	})
}

// Ping checks Docker connectivity.
func (b *Backend) Ping(ctx context.Context) error {
	_, err := b.cli.Ping(ctx)
	return err
}

func (b *Backend) containerIP(ctx context.Context, sandboxID string) (string, error) {
	insp, err := b.cli.ContainerInspect(ctx, containerName(sandboxID))
	if err != nil {
		return "", err
	}
	if insp.NetworkSettings == nil {
		return "", fmt.Errorf("container has no network settings")
	}
	if ip := insp.NetworkSettings.IPAddress; ip != "" {
		return ip, nil
	}
	for _, n := range insp.NetworkSettings.Networks {
		if n != nil && n.IPAddress != "" {
			return n.IPAddress, nil
		}
	}
	return "", fmt.Errorf("container has no IP address")
}

func (b *Backend) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	if destPort <= 0 || destPort > 65535 {
		return nil, fmt.Errorf("invalid port %d", destPort)
	}
	ip, err := b.containerIP(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(destPort))

	if strings.HasPrefix(b.host, "ssh://") {
		return dialSSHTunnel(ctx, b.host, addr)
	}

	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

func dialSSHTunnel(ctx context.Context, dockerHost, dest string) (net.Conn, error) {
	tg, err := config.ParseSSHURL(dockerHost)
	if err != nil {
		return nil, err
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=30",
		"-W", dest,
		tg.String(),
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	return &sshTunnelConn{
		stdin:  stdin,
		stdout: stdout,
		cmd:    cmd,
	}, nil
}

type sshTunnelConn struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd
}

func (c *sshTunnelConn) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *sshTunnelConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }
func (c *sshTunnelConn) Close() error {
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
	return nil
}
func (c *sshTunnelConn) LocalAddr() net.Addr                { return pipeAddr("ssh-local") }
func (c *sshTunnelConn) RemoteAddr() net.Addr               { return pipeAddr("ssh-remote") }
func (c *sshTunnelConn) SetDeadline(t time.Time) error      { return nil }
func (c *sshTunnelConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *sshTunnelConn) SetWriteDeadline(t time.Time) error { return nil }

type pipeAddr string

func (a pipeAddr) Network() string { return "ssh" }
func (a pipeAddr) String() string  { return string(a) }

func (b *Backend) AttachPTY(ctx context.Context, sandboxID, sessionKey string, opts backend.PTYOpts, stdin io.Reader, stdout io.Writer) error {
	cmd := opts.Cmd
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh", "-l"}
	}
	workdir := opts.WorkDir
	if workdir == "" {
		workdir = "/workspace"
	}
	rows, cols := opts.Rows, opts.Cols
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}

	createResp, err := b.cli.ContainerExecCreate(ctx, containerName(sandboxID), types.ExecConfig{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Env:          opts.Env,
		WorkingDir:   workdir,
		Cmd:          cmd,
	})
	if err != nil {
		return fmt.Errorf("pty exec create: %w", err)
	}

	if sessionKey != "" {
		b.ptyMu.Lock()
		b.ptys[sessionKey] = createResp.ID
		b.ptyMu.Unlock()
		defer func() {
			b.ptyMu.Lock()
			delete(b.ptys, sessionKey)
			b.ptyMu.Unlock()
		}()
	}

	attach, err := b.cli.ContainerExecAttach(ctx, createResp.ID, types.ExecStartCheck{Tty: true})
	if err != nil {
		return fmt.Errorf("pty exec attach: %w", err)
	}
	defer attach.Close()

	_ = b.cli.ContainerExecResize(ctx, createResp.ID, container.ResizeOptions{
		Height: uint(rows),
		Width:  uint(cols),
	})

	errCh := make(chan error, 2)
	go func() {
		_, copyErr := io.Copy(attach.Conn, stdin)
		_ = attach.CloseWrite()
		errCh <- copyErr
	}()
	go func() {
		_, copyErr := io.Copy(stdout, attach.Reader)
		errCh <- copyErr
	}()

	select {
	case <-ctx.Done():
		attach.Close()
		return ctx.Err()
	case err := <-errCh:
		attach.Close()
		if err != nil && ctx.Err() == nil && err != io.EOF {
			return err
		}
		return nil
	}
}

func (b *Backend) ResizePTY(ctx context.Context, sandboxID, sessionKey string, rows, cols uint16) error {
	_ = sandboxID
	b.ptyMu.Lock()
	execID := b.ptys[sessionKey]
	b.ptyMu.Unlock()
	if execID == "" {
		return fmt.Errorf("pty session %q not found", sessionKey)
	}
	return b.cli.ContainerExecResize(ctx, execID, container.ResizeOptions{
		Height: uint(rows),
		Width:  uint(cols),
	})
}

var _ backend.Backend = (*Backend)(nil)
