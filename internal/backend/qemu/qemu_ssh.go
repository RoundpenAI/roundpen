package qemu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

const (
	defaultSSHUser = "roundpen"
	defaultSSHPass = "roundpen"
)

func sshUser() string {
	if v := strings.TrimSpace(os.Getenv("ROUNDPEN_QEMU_SSH_USER")); v != "" {
		return v
	}
	return defaultSSHUser
}

func sshPass() string {
	if v := os.Getenv("ROUNDPEN_QEMU_SSH_PASSWORD"); v != "" {
		return v
	}
	return defaultSSHPass
}

func (b *Backend) sshHostPort(sandboxID string) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := b.vms[sandboxID]
	if v == nil {
		raw, err := os.ReadFile(b.statePath(sandboxID))
		if err != nil {
			return 0, fmt.Errorf("qemu: unknown sandbox %s", sandboxID)
		}
		v = &vm{}
		if err := json.Unmarshal(raw, v); err != nil {
			return 0, err
		}
		b.vms[sandboxID] = v
	}
	if v.SSHHostPort > 0 {
		return v.SSHHostPort, nil
	}
	if v.Ports != nil {
		if p := v.Ports[sshGuestPort]; p > 0 {
			return p, nil
		}
	}
	return 0, fmt.Errorf("qemu: no SSH hostfwd for %s (start the VM first)", sandboxID)
}

func (b *Backend) waitSSH(ctx context.Context, sandboxID string) (int, error) {
	port, err := b.sshHostPort(sandboxID)
	if err != nil {
		return 0, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	var d net.Dialer
	deadline, ok := ctx.Deadline()
	if !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	for {
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return port, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return 0, fmt.Errorf("qemu: wait SSH %s: %w", addr, err)
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("qemu: wait SSH %s: %w", addr, ctx.Err())
		case <-time.After(400 * time.Millisecond):
		}
	}
}

func sshClientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            sshUser(),
		Auth:            []ssh.AuthMethod{ssh.Password(sshPass())},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func attachShell(opts backend.AttachExecOpts) (string, error) {
	if len(opts.Cmd) == 0 {
		return "", fmt.Errorf("cmd is required")
	}
	var b strings.Builder
	b.WriteString("set -a; [ -f /etc/roundpen/env ] && . /etc/roundpen/env; set +a; ")
	for k, v := range opts.Env {
		if k == "" {
			continue
		}
		fmt.Fprintf(&b, "export %s=%s; ", k, shellQuote(v))
	}
	wd := opts.WorkDir
	if wd == "" {
		wd = "/workspace"
	}
	fmt.Fprintf(&b, "cd %s && exec", shellQuote(wd))
	for _, a := range opts.Cmd {
		b.WriteByte(' ')
		b.WriteString(shellQuote(a))
	}
	return b.String(), nil
}

const mountWorkspaceDiskCmd = `set -e
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 20; do
  if [ -b /dev/vdb ]; then break; fi
  sleep 0.4
done
if [ ! -b /dev/vdb ]; then
  echo "qemu: /dev/vdb (workspace disk) not present" >&2
  exit 1
fi
if ! sudo blkid /dev/vdb >/dev/null 2>&1; then
  sudo mkfs.ext4 -F -L workspace /dev/vdb
fi
sudo mkdir -p /workspace
if grep -q ' workspace /workspace 9p ' /proc/mounts 2>/dev/null; then
  sudo umount /workspace || true
fi
if ! grep -q ' /workspace ' /proc/mounts 2>/dev/null; then
  sudo mount /dev/vdb /workspace
fi
sudo chown roundpen:roundpen /workspace
`

const mountWorkspaceCmd = `set -e
if grep -q ' workspace /workspace 9p ' /proc/mounts 2>/dev/null; then
  exit 0
fi
sudo mkdir -p /workspace
sudo modprobe 9pnet_virtio 9p 2>/dev/null || true
sudo mount -t 9p -o trans=virtio,version=9p2000.L,msize=262144,rw workspace /workspace
`

// prepareGuestCmd fixes DNS and Claude Code policy on existing images
// (empty /etc/resolv.conf from the Docker-exported rootfs; no image rebuild).
const prepareGuestCmd = `set -e
if ! grep -q '^nameserver ' /etc/resolv.conf 2>/dev/null; then
  sudo tee /etc/resolv.conf >/dev/null <<'EOF'
nameserver 10.0.2.3
nameserver 8.8.8.8
EOF
fi
sudo python3 - <<'PY'
import json, os
for path in ("/etc/claude-code/managed-settings.json", "/home/roundpen/.claude/settings.json"):
    data = {}
    try:
        with open(path, encoding="utf-8") as f:
            loaded = json.load(f)
        if isinstance(loaded, dict):
            data = loaded
    except Exception:
        pass
    perm = data.get("permissions")
    if not isinstance(perm, dict):
        perm = {}
    perm["defaultMode"] = "bypassPermissions"
    data["permissions"] = perm
    data["sandbox"] = {"enabled": False}
    data["skipDangerousModePermissionPrompt"] = True
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as out:
        json.dump(data, out, indent=2)
        out.write("\n")
PY
sudo chown -R roundpen:roundpen /home/roundpen/.claude 2>/dev/null || true
`

func (b *Backend) ensureGuestReady(ctx context.Context, sandboxID string) error {
	b.mu.Lock()
	v := b.vms[sandboxID]
	mountWS := v != nil && strings.TrimSpace(v.Workspace) != "" && strings.TrimSpace(v.WorkspaceDisk) == ""
	mountDisk := v != nil && strings.TrimSpace(v.WorkspaceDisk) != ""
	b.mu.Unlock()
	port, err := b.waitSSH(ctx, sandboxID)
	if err != nil {
		return err
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), sshClientConfig())
	if err != nil {
		return fmt.Errorf("qemu ssh prepare: %w", err)
	}
	defer client.Close()
	if err := sshRun(client, prepareGuestCmd); err != nil {
		return fmt.Errorf("qemu guest dns/settings: %w", err)
	}
	if mountDisk {
		if err := sshRun(client, mountWorkspaceDiskCmd); err != nil {
			return fmt.Errorf("qemu mount workspace disk: %w", err)
		}
	} else if mountWS {
		if err := sshRun(client, mountWorkspaceCmd); err != nil {
			return fmt.Errorf("qemu 9p mount /workspace: %w", err)
		}
	}
	return nil
}

func sshRun(client *ssh.Client, cmd string) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	return sess.Run(cmd)
}

func (b *Backend) AttachExec(ctx context.Context, sandboxID string, opts backend.AttachExecOpts, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd, err := attachShell(opts)
	if err != nil {
		return err
	}
	_ = b.ensureGuestReady(ctx, sandboxID)
	port, err := b.waitSSH(ctx, sandboxID)
	if err != nil {
		return err
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), sshClientConfig())
	if err != nil {
		return fmt.Errorf("qemu ssh: %w", err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("qemu ssh session: %w", err)
	}
	defer sess.Close()
	if stdin != nil {
		sess.Stdin = stdin
	}
	if stdout != nil {
		sess.Stdout = stdout
	}
	if stderr != nil {
		sess.Stderr = stderr
	}
	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("qemu ssh start: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		_ = sess.Close()
		return ctx.Err()
	case err := <-done:
		if err != nil && ctx.Err() == nil {
			return err
		}
		return nil
	}
}
