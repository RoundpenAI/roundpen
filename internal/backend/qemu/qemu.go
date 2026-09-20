// Package qemu runs sandboxes as QEMU virtual machines.
//
// Desktop/Mobile slot VMs use a qcow2 disk and are kernel-booted
// (vmlinuz + initrd sidecars next to the disk artifact).
// They are not BIOS/GRUB disks.
//
// Networking is QEMU user-mode slirp: the host is 10.0.2.2 from the guest.
// CreateOpts.Env is written to fw_cfg opt/roundpen/env (loopback URLs rewritten
// to 10.0.2.2) so Claude Code / guest agents talk to Roundpen llmgw.
// Chrome CDP (:9222) and SSH (:22) are hostfwd'd to 127.0.0.1 on the host.
// The guest desktop is QEMU native VNC on a Unix socket.
package qemu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend"
	"golang.org/x/crypto/ssh"
)

const (
	defaultMemoryMB = 2048
	defaultCPUs     = 2
	cdpGuestPort    = 9222
	sshGuestPort    = 22
	hostfwdBase     = 19222
	minImageBytes   = 32 << 20
	slirpHost       = "10.0.2.2"
	workspaceDiskGB = 20
)

// Backend launches and manages QEMU VMs under dataRoot/qemu/{sandboxID}.
type Backend struct {
	dataRoot string
	qemuBin  string
	mu       sync.Mutex
	vms      map[string]*vm
	nextPort int
}

type vm struct {
	SandboxID     string      `json:"sandbox_id"`
	PID           int         `json:"pid"`
	Image         string      `json:"image"`
	Disk          string      `json:"disk"`
	VNCSock       string      `json:"vnc_sock"`
	CDPHostPort   int         `json:"cdp_host_port"`
	SSHHostPort   int         `json:"ssh_host_port,omitempty"`
	Ports         map[int]int `json:"ports"` // guest -> host
	MemoryMB      int         `json:"memory_mb,omitempty"`
	CPUs          int         `json:"cpus,omitempty"`
	Kernel        string      `json:"kernel,omitempty"`
	Initrd        string      `json:"initrd,omitempty"`
	Append        string      `json:"append,omitempty"`
	EnvFile       string      `json:"env_file,omitempty"`
	Workspace     string      `json:"workspace,omitempty"`      // host path exported via 9p (browser)
	WorkspaceDisk string      `json:"workspace_disk,omitempty"` // qcow2 attached as virtio /dev/vdb (agent)
	cmd           *exec.Cmd   `json:"-"`
}

// New returns a QEMU backend. dataRoot is the Roundpen data directory.
func New(dataRoot string) (*Backend, error) {
	if dataRoot == "" {
		return nil, fmt.Errorf("qemu: data root is required")
	}
	if err := BinariesAvailable(); err != nil {
		return nil, err
	}
	bin := qemuSystemBin()
	root := filepath.Join(dataRoot, "qemu")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	b := &Backend{
		dataRoot: dataRoot,
		qemuBin:  bin,
		vms:      make(map[string]*vm),
		nextPort: hostfwdBase,
	}
	_ = b.recover()
	return b, nil
}

func (b *Backend) Name() string { return "qemu" }

func (b *Backend) dir(id string) string {
	return filepath.Join(b.dataRoot, "qemu", id)
}

func (b *Backend) statePath(id string) string {
	return filepath.Join(b.dir(id), "state.json")
}

func (b *Backend) recover() error {
	root := filepath.Join(b.dataRoot, "qemu")
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		raw, err := os.ReadFile(b.statePath(id))
		if err != nil {
			continue
		}
		var v vm
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		if v.PID > 0 && processAlive(v.PID) {
			b.vms[id] = &v
			b.bumpPort(v.CDPHostPort)
			b.bumpPort(v.SSHHostPort)
			for _, p := range v.Ports {
				b.bumpPort(p)
			}
		}
	}
	return nil
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func (b *Backend) save(v *vm) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(b.statePath(v.SandboxID), raw, 0o600)
}

func (b *Backend) bumpPort(p int) {
	if p >= b.nextPort {
		b.nextPort = p + 1
	}
}

func (b *Backend) allocPort() int {
	p := b.nextPort
	b.nextPort++
	return p
}

// Create provisions a VM directory and overlay disk; Start launches QEMU.
func (b *Backend) Create(ctx context.Context, opts backend.CreateOpts) (string, error) {
	_ = ctx
	if opts.SandboxID == "" {
		return "", fmt.Errorf("qemu: sandbox id required")
	}
	img, err := resolveImage(opts.Image)
	if err != nil {
		return "", err
	}
	if err := checkImage(img); err != nil {
		return "", err
	}
	boot, err := loadBootConfig(img)
	if err != nil {
		return "", err
	}

	dir := b.dir(opts.SandboxID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	disk := filepath.Join(dir, "disk.qcow2")
	if _, err := os.Stat(disk); err != nil {
		cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", img, disk)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("qemu-img create overlay: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	envFile := filepath.Join(dir, "guest.env")
	if err := writeGuestEnv(envFile, mergeGuestEnv(opts)); err != nil {
		return "", err
	}

	ws := strings.TrimSpace(opts.MountDir)
	if ws != "" {
		if abs, err := filepath.Abs(ws); err == nil {
			ws = abs
		}
		if strings.ContainsAny(ws, ",\n\r") {
			return "", fmt.Errorf("qemu: workspace path is not exportable")
		}
		if err := os.MkdirAll(ws, 0o755); err != nil {
			return "", fmt.Errorf("qemu: workspace: %w", err)
		}
	}

	var wsDisk string
	if useVirtioWorkspace(opts) {
		if ws != "" {
			wsDisk = filepath.Join(filepath.Dir(ws), "workspace.qcow2")
		} else {
			wsDisk = filepath.Join(dir, "workspace.qcow2")
		}
		if err := ensureWorkspaceDisk(wsDisk); err != nil {
			return "", err
		}
		ws = "" // do not also export 9p — guest owns the filesystem
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	cdpHost := b.allocPort()
	sshHost := b.allocPort()
	v := &vm{
		SandboxID:     opts.SandboxID,
		Image:         img,
		Disk:          disk,
		VNCSock:       filepath.Join(dir, "vnc.sock"),
		CDPHostPort:   cdpHost,
		SSHHostPort:   sshHost,
		Ports:         map[int]int{cdpGuestPort: cdpHost, sshGuestPort: sshHost},
		MemoryMB:      memoryMBFromOpts(opts),
		CPUs:          cpusFromOpts(opts),
		Kernel:        boot.Kernel,
		Initrd:        boot.Initrd,
		Append:        boot.Append,
		EnvFile:       envFile,
		Workspace:     ws,
		WorkspaceDisk: wsDisk,
	}
	b.vms[opts.SandboxID] = v
	if err := b.save(v); err != nil {
		return "", err
	}
	return opts.SandboxID, nil
}

func (b *Backend) Start(ctx context.Context, sandboxID string) error {
	_ = ctx
	b.mu.Lock()
	v, ok := b.vms[sandboxID]
	if !ok {
		raw, err := os.ReadFile(b.statePath(sandboxID))
		if err != nil {
			b.mu.Unlock()
			return fmt.Errorf("qemu: unknown sandbox %s", sandboxID)
		}
		v = &vm{}
		if err := json.Unmarshal(raw, v); err != nil {
			b.mu.Unlock()
			return err
		}
		b.vms[sandboxID] = v
	}
	if v.PID > 0 && processAlive(v.PID) {
		b.mu.Unlock()
		return nil
	}
	_ = os.Remove(v.VNCSock)

	args := qemuArgs(v, kvmAvailable())
	logPath := filepath.Join(b.dir(sandboxID), "qemu-start.log")
	logf, err := os.Create(logPath)
	if err != nil {
		b.mu.Unlock()
		return err
	}
	cmd := exec.Command(b.qemuBin, args...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	b.mu.Unlock()

	runErr := cmd.Run()
	_ = logf.Close()
	if runErr != nil {
		tail, _ := os.ReadFile(logPath)
		return fmt.Errorf("qemu start: %w: %s", runErr, strings.TrimSpace(string(tail)))
	}

	pidRaw, err := os.ReadFile(filepath.Join(b.dir(sandboxID), "qemu.pid"))
	if err != nil {
		return fmt.Errorf("qemu pidfile: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidRaw)))
	if err != nil {
		return fmt.Errorf("qemu pidfile parse: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	v.PID = pid
	if err := b.save(v); err != nil {
		return err
	}
	sid := sandboxID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_ = b.ensureGuestReady(ctx, sid)
	}()
	return nil
}

// Running reports whether the sandbox VM process is alive.
func (b *Backend) Running(ctx context.Context, sandboxID string) (bool, error) {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	v := b.vms[sandboxID]
	if v == nil {
		return false, nil
	}
	return v.PID > 0 && processAlive(v.PID), nil
}

func (b *Backend) RefreshImage(ctx context.Context, ref string) (bool, string, error) {
	_ = ctx
	return false, "", fmt.Errorf("image refresh is not supported for qemu images")
}

func (b *Backend) Stop(ctx context.Context, sandboxID string) error {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	v := b.vms[sandboxID]
	if v == nil {
		return nil
	}
	if v.PID > 0 {
		_ = syscall.Kill(v.PID, syscall.SIGTERM)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && processAlive(v.PID) {
			time.Sleep(100 * time.Millisecond)
		}
		if processAlive(v.PID) {
			_ = syscall.Kill(v.PID, syscall.SIGKILL)
		}
		v.PID = 0
		_ = b.save(v)
	}
	_ = os.Remove(v.VNCSock)
	return nil
}

func (b *Backend) Remove(ctx context.Context, sandboxID string) error {
	_ = b.Stop(ctx, sandboxID)
	b.mu.Lock()
	delete(b.vms, sandboxID)
	b.mu.Unlock()
	return os.RemoveAll(b.dir(sandboxID))
}

func (b *Backend) Exec(ctx context.Context, sandboxID string, opts backend.ExecOpts) (*backend.ExecResult, error) {
	cmd, err := attachShell(backend.AttachExecOpts{Cmd: opts.Cmd, WorkDir: opts.WorkDir, Env: opts.Env})
	if err != nil {
		return nil, err
	}
	_ = b.ensureGuestReady(ctx, sandboxID)
	port, err := b.waitSSH(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), sshClientConfig())
	if err != nil {
		return nil, fmt.Errorf("qemu ssh: %w", err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("qemu ssh session: %w", err)
	}
	defer sess.Close()

	var stdout, stderr strings.Builder
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	runErr := sess.Run(cmd)
	res := &backend.ExecResult{Stdout: []byte(stdout.String()), Stderr: []byte(stderr.String())}
	if runErr == nil {
		return res, nil
	}
	if ee, ok := runErr.(*ssh.ExitError); ok {
		res.ExitCode = ee.ExitStatus()
		return res, nil
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	return res, runErr
}

func (b *Backend) Logs(ctx context.Context, sandboxID string) (io.ReadCloser, error) {
	_ = ctx
	serial := filepath.Join(b.dir(sandboxID), "serial.log")
	if f, err := os.Open(serial); err == nil {
		return f, nil
	}
	path := filepath.Join(b.dir(sandboxID), "qemu.log")
	f, err := os.Open(path)
	if err != nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return f, nil
}

// Dial opens a TCP connection to a guest port via user-mode hostfwd.
// Currently CDP (9222) is always forwarded; other ports return an error.
func (b *Backend) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	b.mu.Lock()
	v := b.vms[sandboxID]
	if v == nil {
		raw, err := os.ReadFile(b.statePath(sandboxID))
		if err == nil {
			v = &vm{}
			_ = json.Unmarshal(raw, v)
			b.vms[sandboxID] = v
		}
	}
	hostPort := 0
	if v != nil {
		if p, ok := v.Ports[destPort]; ok {
			hostPort = p
		} else if destPort == cdpGuestPort {
			hostPort = v.CDPHostPort
		}
	}
	b.mu.Unlock()
	if hostPort <= 0 {
		return nil, fmt.Errorf("qemu: no hostfwd for guest port %d", destPort)
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", hostPort))
}

// VNCSock returns the Unix socket path for QEMU VNC, if the VM exists.
func (b *Backend) VNCSock(sandboxID string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := b.vms[sandboxID]
	if v == nil {
		raw, err := os.ReadFile(b.statePath(sandboxID))
		if err != nil {
			return "", fmt.Errorf("qemu: unknown sandbox %s", sandboxID)
		}
		v = &vm{}
		if err := json.Unmarshal(raw, v); err != nil {
			return "", err
		}
		b.vms[sandboxID] = v
	}
	if v.VNCSock == "" {
		return "", fmt.Errorf("qemu: no vnc sock for %s", sandboxID)
	}
	return v.VNCSock, nil
}

func (b *Backend) AttachPTY(ctx context.Context, sandboxID, sessionKey string, opts backend.PTYOpts, stdin io.Reader, stdout io.Writer) error {
	_ = ctx
	_ = sandboxID
	_ = sessionKey
	_ = opts
	_ = stdin
	_ = stdout
	return fmt.Errorf("qemu: AttachPTY not supported yet")
}

func (b *Backend) ResizePTY(ctx context.Context, sandboxID, sessionKey string, rows, cols uint16) error {
	_ = ctx
	_ = sandboxID
	_ = sessionKey
	_ = rows
	_ = cols
	return fmt.Errorf("qemu: ResizePTY not supported")
}

// AttachExec is implemented in qemu_ssh.go (SSH hostfwd :22).

// VNCDialer is implemented by backends that expose a desktop via Unix VNC.
type VNCDialer interface {
	VNCSock(sandboxID string) (string, error)
}
