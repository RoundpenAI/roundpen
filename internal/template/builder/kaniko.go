package builder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
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
	// ExtraArgs are appended to the executor command line.
	ExtraArgs []string
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
func (k *Kaniko) Build(ctx context.Context, baseImage string, spec Spec, tag string, log LogFn) (artifact string, snapshot bool, err error) {
	if log == nil {
		log = func(_, _, _ string) {}
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

	dest := JoinImageRef(k.cfg.DestinationPrefix, tag)
	args := []string{
		"--context=" + DirContextURI(ctxDir),
		"--dockerfile=Dockerfile",
		"--destination=" + dest,
		"--cache=false",
		"--snapshot-mode=redo",
	}
	if k.cfg.Insecure {
		args = append(args, "--insecure")
	}
	if k.cfg.SkipTLSVerify {
		args = append(args, "--skip-tls-verify")
	}
	args = append(args, k.cfg.ExtraArgs...)

	log("info", "kaniko", "executing "+k.cfg.Executor+" --destination="+dest)
	cmd := exec.CommandContext(ctx, k.cfg.Executor, args...)
	cmd.Dir = ctxDir
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

	if spec.StartCmd != "" {
		log("warn", "snapshot", "T2 snapshot verification requires docker builder; image pushed without snapshot")
	}
	return dest, false, nil
}

func streamCmdLogs(r io.Reader, log LogFn, step string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		log("info", step, line)
	}
}
