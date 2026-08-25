// Package sshfs implements WorkspaceFS on a remote host over SSH.
// Paths are Unix-style and resolved on the remote Docker host so bind mounts work.
package sshfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// Runner executes a remote shell command via SSH.
type Runner interface {
	Run(ctx context.Context, script string, stdin io.Reader) (stdout []byte, err error)
}

// FS stores workspaces under Root/workspaces/{id} on a remote host.
type FS struct {
	Root   string // absolute Unix path on remote host
	Runner Runner
	// DirMode is the remote directory mode for new workspaces (default 0777
	// so containers can write regardless of host/container UID mapping).
	DirMode os.FileMode
}

// New builds an SSH-backed FS for target with remote root.
func New(target config.SSHTarget, root string) (*FS, error) {
	if root == "" || !path.IsAbs(root) {
		return nil, fmt.Errorf("sshfs root must be an absolute Unix path, got %q", root)
	}
	return &FS{
		Root:    path.Clean(root),
		Runner:  &sshRunner{target: target},
		DirMode: 0o777,
	}, nil
}

// NewFromDockerHost parses ssh://DOCKER_HOST and returns an FS.
func NewFromDockerHost(dockerHost, root string) (*FS, error) {
	tg, err := config.ParseSSHURL(dockerHost)
	if err != nil {
		return nil, err
	}
	return New(tg, root)
}

func (f *FS) base(id string) string {
	return path.Join(f.Root, "workspaces", id)
}

func (f *FS) resolve(id, relPath string) (string, error) {
	base := f.base(id)
	cleaned := path.Clean(strings.TrimSpace(relPath))
	if cleaned == "." || cleaned == "" {
		return base, nil
	}
	if path.IsAbs(cleaned) {
		cleaned = strings.TrimPrefix(cleaned, "/")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path escapes workspace")
	}
	full := path.Join(base, cleaned)
	if full != base && !strings.HasPrefix(full, base+"/") {
		return "", fmt.Errorf("path escapes workspace")
	}
	return full, nil
}

func (f *FS) mode() os.FileMode {
	if f.DirMode == 0 {
		return 0o777
	}
	return f.DirMode
}

func (f *FS) Create(ctx context.Context, id string, ephemeral bool) (*workspace.Info, error) {
	p := f.base(id)
	mode := f.mode() & 0o777
	script := fmt.Sprintf("mkdir -p %s %s && chmod %04o %s %s",
		shellQuote(f.Root), shellQuote(p), mode, shellQuote(f.Root), shellQuote(p),
	)
	if _, err := f.Runner.Run(ctx, script, nil); err != nil {
		return nil, fmt.Errorf("sshfs create: %w", err)
	}
	return &workspace.Info{ID: id, HostPath: p, Ephemeral: ephemeral}, nil
}

func (f *FS) Get(ctx context.Context, id string) (*workspace.Info, error) {
	p := f.base(id)
	script := fmt.Sprintf("if [ -d %s ]; then echo OK; else echo MISSING; fi", shellQuote(p))
	out, err := f.Runner.Run(ctx, script, nil)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(out), "OK") {
		return nil, os.ErrNotExist
	}
	return &workspace.Info{ID: id, HostPath: p}, nil
}

func (f *FS) Remove(ctx context.Context, id string) error {
	p := f.base(id)
	script := fmt.Sprintf("rm -rf %s", shellQuote(p))
	_, err := f.Runner.Run(ctx, script, nil)
	return err
}

func (f *FS) Open(ctx context.Context, id, relPath string) (io.ReadCloser, error) {
	p, err := f.resolve(id, relPath)
	if err != nil {
		return nil, err
	}
	script := fmt.Sprintf("cat %s", shellQuote(p))
	out, err := f.Runner.Run(ctx, script, nil)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(out)), nil
}

func (f *FS) Write(ctx context.Context, id, relPath string, r io.Reader) error {
	p, err := f.resolve(id, relPath)
	if err != nil {
		return err
	}
	dir := path.Dir(p)
	mode := f.mode() & 0o777
	script := fmt.Sprintf("mkdir -p %s && chmod %04o %s && cat > %s",
		shellQuote(dir), mode, shellQuote(dir), shellQuote(p),
	)
	_, err = f.Runner.Run(ctx, script, r)
	return err
}

func (f *FS) Stat(ctx context.Context, id, relPath string) (os.FileInfo, error) {
	p, err := f.resolve(id, relPath)
	if err != nil {
		return nil, err
	}
	// type|size|mtime|name
	script := fmt.Sprintf(
		`if [ ! -e %s ]; then echo MISSING; exit 0; fi; `+
			`if [ -d %s ]; then t=dir; else t=file; fi; `+
			`stat -c "$t|%%s|%%Y|%%n" %s`,
		shellQuote(p), shellQuote(p), shellQuote(p),
	)
	out, err := f.Runner.Run(ctx, script, nil)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(out))
	if s == "MISSING" || s == "" {
		return nil, os.ErrNotExist
	}
	parts := strings.SplitN(s, "|", 4)
	if len(parts) < 4 {
		return nil, fmt.Errorf("sshfs stat parse: %q", s)
	}
	size, _ := strconv.ParseInt(parts[1], 10, 64)
	mtime, _ := strconv.ParseInt(parts[2], 10, 64)
	return remoteFileInfo{
		name:  path.Base(parts[3]),
		size:  size,
		mode:  modeFromType(parts[0]),
		mtime: time.Unix(mtime, 0),
	}, nil
}

type remoteFileInfo struct {
	name  string
	size  int64
	mode  os.FileMode
	mtime time.Time
}

func (i remoteFileInfo) Name() string       { return i.name }
func (i remoteFileInfo) Size() int64        { return i.size }
func (i remoteFileInfo) Mode() os.FileMode  { return i.mode }
func (i remoteFileInfo) ModTime() time.Time { return i.mtime }
func (i remoteFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i remoteFileInfo) Sys() any           { return nil }

func modeFromType(t string) os.FileMode {
	if t == "dir" {
		return os.ModeDir | 0o755
	}
	return 0o644
}

type sshRunner struct {
	target config.SSHTarget
}

func (r *sshRunner) Run(ctx context.Context, script string, stdin io.Reader) ([]byte, error) {
	// Pass a single remote command string so the login shell parses quotes.
	remote := "exec bash -lc " + shellQuote(script)
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=30",
		r.target.String(),
		remote,
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh %s: %w: %s", r.target.String(), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

var _ workspace.FS = (*FS)(nil)
