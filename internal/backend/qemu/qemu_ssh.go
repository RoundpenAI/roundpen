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

func (b *Backend) AttachExec(ctx context.Context, sandboxID string, opts backend.AttachExecOpts, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd, err := attachShell(opts)
	if err != nil {
		return err
	}
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
