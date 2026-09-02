package builder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// KanikoConfig configures the Kaniko executor builder.
type KanikoConfig struct {
	// Executor is the kaniko executor binary (default: "executor").
	Executor string
	// DestinationPrefix is the registry/repo prefix, e.g. registry.example.com/roundpen.
	DestinationPrefix string
	// Insecure allows plain-HTTP registries.
	Insecure bool
	// SkipTLSVerify skips TLS verification when pushing.
	SkipTLSVerify bool
	// RegistryMirrors are passed as repeated --registry-mirror (Docker Hub pull-through).
	RegistryMirrors []string
	// ExtraArgs are appended to the executor command line.
	ExtraArgs []string
	// NoSandbox runs the executor directly on the host (tests / recovery only).
	// Production builds use bubblewrap so Kaniko gets an isolated rootfs and can
	// chown without being real root or writing into the executor's install dir.
	NoSandbox bool
}

// Kaniko builds template images using the Kaniko executor (no Docker daemon).
type Kaniko struct {
	cfg KanikoConfig
}

// NewKaniko validates config and returns a Kaniko builder.
func NewKaniko(cfg KanikoConfig) (*Kaniko, error) {
	if strings.TrimSpace(cfg.DestinationPrefix) == "" {
		return nil, fmt.Errorf("kaniko destination prefix is required")
	}
	if cfg.Executor == "" {
		cfg.Executor = "executor"
	}
	if _, err := exec.LookPath(cfg.Executor); err != nil {
		return nil, fmt.Errorf("kaniko executor %q not found on PATH", cfg.Executor)
	}
	cfg.DestinationPrefix = SanitizeImageRef(cfg.DestinationPrefix)
	return &Kaniko{cfg: cfg}, nil
}

// Close is a no-op for Kaniko.
func (k *Kaniko) Close() error { return nil }

// Build runs Kaniko executor against a generated Dockerfile context.
// tags are local refs (name:tag); each is pushed under DestinationPrefix.
// The first tag is returned as the primary artifact.
func (k *Kaniko) Build(ctx context.Context, baseImage string, spec Spec, tags []string, log LogFn) (artifact string, snapshot bool, err error) {
	if log == nil {
		log = func(_, _, _ string) {}
	}
	if len(tags) == 0 {
		return "", false, fmt.Errorf("kaniko build requires at least one image tag")
	}
	df, err := Dockerfile(baseImage, spec)
	if err != nil {
		return "", false, err
	}
	log("info", "dockerfile", "generated Dockerfile")

	ctxDir, cleanup, err := WriteBuildContext(df)
	if err != nil {
		return "", false, err
	}
	defer cleanup()

	dests := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		dests = append(dests, JoinImageRef(k.cfg.DestinationPrefix, tag))
	}
	if len(dests) == 0 {
		return "", false, fmt.Errorf("kaniko build requires at least one image tag")
	}
	primary := dests[0]

	execArgs := []string{
		"--context=" + DirContextURI("/workspace"),
		"--dockerfile=Dockerfile",
		"--cache=false",
		"--snapshot-mode=redo",
	}
	for _, dest := range dests {
		execArgs = append(execArgs, "--destination="+dest)
	}
	if k.cfg.Insecure {
		execArgs = append(execArgs, "--insecure")
	}
	if k.cfg.SkipTLSVerify {
		execArgs = append(execArgs, "--skip-tls-verify")
	}
	for _, m := range k.cfg.RegistryMirrors {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		execArgs = append(execArgs, "--registry-mirror="+m)
	}
	execArgs = append(execArgs, k.cfg.ExtraArgs...)

	executor, err := exec.LookPath(k.cfg.Executor)
	if err != nil {
		return "", false, fmt.Errorf("kaniko executor: %w", err)
	}

	var cmd *exec.Cmd
	var sandboxCleanup func()
	if !k.cfg.NoSandbox {
		if _, err := exec.LookPath("bwrap"); err != nil {
			return "", false, fmt.Errorf("kaniko builds require bubblewrap (bwrap) for an isolated rootfs; install bubblewrap, or set ROUNDPEN_TEMPLATE_BUILDER=docker")
		}
		log("info", "kaniko", "executing sandboxed "+executor+" --destination="+strings.Join(dests, ","))
		cmd, sandboxCleanup, err = k.sandboxedCmd(ctx, executor, ctxDir, execArgs)
		if err != nil {
			return "", false, err
		}
		defer sandboxCleanup()
	} else {
		// Host path: KanikoDir becomes the executor's directory; --force is required
		// outside a container. Prefer the sandboxed path above for real builds.
		hostArgs := append([]string{"--force"}, rewriteWorkspaceArgs(execArgs, ctxDir)...)
		log("info", "kaniko", "executing "+executor+" --destination="+strings.Join(dests, ",")+" (host, no sandbox)")
		cmd = exec.CommandContext(ctx, executor, hostArgs...)
		cmd.Dir = ctxDir
		cmd.Env = kanikoEnv()
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", false, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", false, err
	}
	if err := cmd.Start(); err != nil {
		return "", false, fmt.Errorf("kaniko start: %w", err)
	}
	streamCmdLogs(stdout, log, "kaniko")
	streamCmdLogs(stderr, log, "kaniko")
	if err := cmd.Wait(); err != nil {
		return "", false, fmt.Errorf("kaniko build: %w", err)
	}

	if spec.KeepImageCmd {
		return primary, true, nil
	}
	if spec.StartCmd != "" {
		log("warn", "snapshot", "T2 snapshot verification requires docker builder; image pushed without snapshot")
	}
	return primary, false, nil
}

func rewriteWorkspaceArgs(args []string, ctxDir string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.HasPrefix(a, "--context=") {
			out[i] = "--context=" + DirContextURI(ctxDir)
			continue
		}
		out[i] = a
	}
	return out
}

func kanikoEnv() []string {
	env := os.Environ()
	if os.Getenv("DOCKER_CONFIG") == "" {
		if cfg := dockerConfigDir(); cfg != "" {
			env = append(env, "DOCKER_CONFIG="+cfg)
		}
	}
	return env
}

func (k *Kaniko) sandboxedCmd(ctx context.Context, executor, ctxDir string, execArgs []string) (*exec.Cmd, func(), error) {
	root, err := os.MkdirTemp("", "roundpen-kaniko-root-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }

	for _, d := range []string{
		filepath.Join(root, "kaniko"),
		filepath.Join(root, "kaniko", ".docker"),
		filepath.Join(root, "workspace"),
		filepath.Join(root, "tmp"),
		filepath.Join(root, "etc"),
		filepath.Join(root, "etc", "ssl"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	// Marker so Kaniko's container check succeeds without --force.
	if err := os.WriteFile(filepath.Join(root, ".dockerenv"), nil, 0o644); err != nil {
		cleanup()
		return nil, nil, err
	}
	// Destinations must exist before bwrap can bind over them.
	for _, f := range []string{
		filepath.Join(root, "etc", "resolv.conf"),
		filepath.Join(root, "etc", "hosts"),
	} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "etc", "ssl", "certs"), 0o755); err != nil {
		cleanup()
		return nil, nil, err
	}

	bwrapArgs := []string{
		"--unshare-user",
		"--uid", "0",
		"--gid", "0",
		"--bind", root, "/",
		"--ro-bind", executor, "/kaniko/executor",
		"--bind", ctxDir, "/workspace",
		"--dev", "/dev",
		"--proc", "/proc",
		"--die-with-parent",
		"--setenv", "HOME", "/root",
		"--setenv", "container", "docker",
	}
	if cfg := dockerConfigDir(); cfg != "" {
		bwrapArgs = append(bwrapArgs,
			"--ro-bind", cfg, "/kaniko/.docker",
			"--setenv", "DOCKER_CONFIG", "/kaniko/.docker",
		)
	}
	for _, p := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/ssl/certs"} {
		if _, err := os.Stat(p); err == nil {
			bwrapArgs = append(bwrapArgs, "--ro-bind", p, p)
		}
	}
	bwrapArgs = append(bwrapArgs, "/kaniko/executor")
	bwrapArgs = append(bwrapArgs, execArgs...)

	cmd := exec.CommandContext(ctx, "bwrap", bwrapArgs...)
	cmd.Env = []string{
		"PATH=/kaniko:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"container=docker",
	}
	if cfg := dockerConfigDir(); cfg != "" {
		cmd.Env = append(cmd.Env, "DOCKER_CONFIG=/kaniko/.docker")
	}
	return cmd, cleanup, nil
}

func dockerConfigDir() string {
	if v := strings.TrimSpace(os.Getenv("DOCKER_CONFIG")); v != "" {
		if st, err := os.Stat(v); err == nil && st.IsDir() {
			return v
		}
		return ""
	}
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, ".docker")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".docker")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	return ""
}

func streamCmdLogs(r io.Reader, log LogFn, step string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		log("info", step, line)
	}
}
