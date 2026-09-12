package hostsetup

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// RunnerConfig configures whitelist action execution.
type RunnerConfig struct {
	RepoRoot string
}

// Runner executes whitelist setup actions.
type Runner struct {
	RepoRoot string
	mu       sync.Mutex
	locks    map[string]*sync.Mutex
}

func NewRunner(cfg RunnerConfig) *Runner {
	root := cfg.RepoRoot
	if root == "" {
		root = FindRepoRoot()
	}
	return &Runner{
		RepoRoot: root,
		locks:    map[string]*sync.Mutex{},
	}
}

func (r *Runner) lockFor(id string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locks[id] == nil {
		r.locks[id] = &sync.Mutex{}
	}
	return r.locks[id]
}

// Run executes a whitelist action, streaming combined output to log.
func (r *Runner) Run(ctx context.Context, actionID string, priv Privilege, log io.Writer) error {
	l := r.lockFor(actionID)
	l.Lock()
	defer l.Unlock()

	switch actionID {
	case ActionInstallQEMU:
		if priv != PrivilegeAuto {
			return fmt.Errorf("install_qemu requires privilege=auto (use manual copy-paste flow)")
		}
		argv := installQEMUArgv(os.Geteuid() == 0)
		return r.runCmd(ctx, argv[0], argv[1:], log)
	case ActionBuildAgentImage:
		script := filepath.Join(r.RepoRoot, "images/agent-qemu/build.sh")
		return r.runCmd(ctx, "bash", []string{script}, log)
	case ActionBuildBrowserImage:
		script := filepath.Join(r.RepoRoot, "images/browser-qemu/build.sh")
		return r.runCmd(ctx, "bash", []string{script}, log)
	default:
		return fmt.Errorf("unknown action %q", actionID)
	}
}

func installQEMUArgv(isRoot bool) []string {
	if isRoot {
		return []string{"apt-get", "install", "-y", "qemu-system-x86", "qemu-utils"}
	}
	return []string{"sudo", "-n", "apt-get", "install", "-y", "qemu-system-x86", "qemu-utils"}
}

func (r *Runner) runCmd(ctx context.Context, name string, args []string, log io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if r.RepoRoot != "" {
		cmd.Dir = r.RepoRoot
	}
	if log == nil {
		log = io.Discard
	}
	cmd.Stdout = log
	cmd.Stderr = log
	fmt.Fprintf(log, "$ %s", name)
	for _, a := range args {
		fmt.Fprintf(log, " %s", a)
	}
	fmt.Fprintln(log)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// FindRepoRoot walks up from cwd for images/agent-qemu/build.sh, or uses ROUNDPEN_REPO_ROOT.
func FindRepoRoot() string {
	if v := os.Getenv("ROUNDPEN_REPO_ROOT"); v != "" {
		return v
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "images/agent-qemu/build.sh")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}
