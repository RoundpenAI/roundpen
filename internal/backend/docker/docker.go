// Package docker implements the Docker Daemon Backend (Phase 1 default).
package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

// Backend talks to a local or remote Docker Engine.
type Backend struct {
	cli     *client.Client
	runtime string
}

// New connects to Docker. host may be empty for the SDK default.
func New(host, runtime string) (*Backend, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Backend{cli: cli, runtime: runtime}, nil
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
		Cmd:        []string{"sleep", "infinity"},
		Labels: map[string]string{
			"roundpen.sandbox_id": opts.SandboxID,
		},
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

var _ backend.Backend = (*Backend)(nil)
