package builder

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Docker builds template images using the local Docker engine.
type Docker struct {
	cli *client.Client
}

// NewDocker connects to Docker for template builds.
func NewDocker(host string) (*Docker, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if host != "" {
		helper, err := connhelper.GetConnectionHelper(host)
		if err != nil {
			return nil, fmt.Errorf("docker host %q: %w", host, err)
		}
		if helper != nil {
			httpClient := &http.Client{Transport: &http.Transport{DialContext: helper.Dialer}}
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
	return &Docker{cli: cli}, nil
}

// Close releases the client.
func (d *Docker) Close() error {
	if d.cli == nil {
		return nil
	}
	return d.cli.Close()
}

// Ping checks connectivity.
func (d *Docker) Ping(ctx context.Context) error {
	_, err := d.cli.Ping(ctx)
	return err
}

// Build executes docker build + optional snapshot phase.
func (d *Docker) Build(ctx context.Context, baseImage string, spec Spec, tag string, log LogFn) (artifact string, snapshot bool, err error) {
	if log == nil {
		log = func(_, _, _ string) {}
	}
	df, err := Dockerfile(baseImage, spec)
	if err != nil {
		return "", false, err
	}
	log("info", "dockerfile", "generated Dockerfile")
	buildCtx, err := archiveDockerfile(df)
	if err != nil {
		return "", false, err
	}
	defer buildCtx.Close()

	resp, err := d.cli.ImageBuild(ctx, buildCtx, types.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: "Dockerfile",
		Remove:     true,
	})
	if err != nil {
		return "", false, fmt.Errorf("image build: %w", err)
	}
	defer resp.Body.Close()
	if err := streamBuildLogs(resp.Body, log); err != nil {
		return "", false, err
	}

	snapshot = spec.StartCmd != ""
	if !snapshot {
		return tag, false, nil
	}

	log("info", "snapshot", "running start/ready verification")
	if err := d.verifySnapshot(ctx, tag, spec, log); err != nil {
		return "", false, err
	}
	snapTag := tag + "-snapshot"
	if err := d.tagImage(ctx, tag, snapTag); err != nil {
		return "", false, err
	}
	log("info", "snapshot", "snapshot image ready: "+snapTag)
	return snapTag, true, nil
}

func (d *Docker) verifySnapshot(ctx context.Context, image string, spec Spec, log LogFn) error {
	name := "roundpen-build-" + fmt.Sprintf("%d", time.Now().UnixNano())
	resp, err := d.cli.ContainerCreate(ctx, &container.Config{Image: image}, &container.HostConfig{}, nil, nil, name)
	if err != nil {
		return fmt.Errorf("snapshot container create: %w", err)
	}
	defer func() {
		_ = d.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	}()
	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("snapshot container start: %w", err)
	}

	probe := ReadyShell(spec.ReadyCmd)
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		execResp, err := d.cli.ContainerExecCreate(ctx, resp.ID, types.ExecConfig{
			Cmd:          []string{"/bin/sh", "-c", probe},
			AttachStdout: true,
			AttachStderr: true,
		})
		if err != nil {
			return err
		}
		attach, err := d.cli.ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{})
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, attach.Reader)
		attach.Close()
		insp, err := d.cli.ContainerExecInspect(ctx, execResp.ID)
		if err != nil {
			return err
		}
		if insp.ExitCode == 0 {
			log("info", "ready", "ready probe succeeded")
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("ready probe timed out: %s", probe)
}

func (d *Docker) tagImage(ctx context.Context, src, dst string) error {
	return d.cli.ImageTag(ctx, src, dst)
}

func streamBuildLogs(r io.Reader, log LogFn) error {
	var stdout, stderr strings.Builder
	_, err := stdcopy.StdCopy(&stdout, &stderr, r)
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()+"\n"+stderr.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		log("info", "build", line)
	}
	return err
}
