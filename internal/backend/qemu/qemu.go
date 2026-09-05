// Package qemu runs sandboxes as QEMU virtual machines.
//
// Browser and agent slot VMs use a qcow2 disk and are kernel-booted
// (vmlinuz + initrd sidecars from images/browser-qemu or images/agent-qemu).
// They are not BIOS/GRUB disks.
//
// Networking is QEMU user-mode slirp: the host is 10.0.2.2 from the guest.
// CreateOpts.Env is written to fw_cfg opt/roundpen/env (loopback URLs rewritten
// to 10.0.2.2) so Claude Code / guest agents talk to Roundpen llmgw.
// Chrome CDP (:9222) and SSH (:22) are hostfwd'd to 127.0.0.1 on the host.
// The guest desktop (browser images) is QEMU native VNC on a Unix socket.
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

const (
	defaultMemoryMB = 2048
	defaultCPUs     = 2
	cdpGuestPort    = 9222
	sshGuestPort    = 22
	hostfwdBase     = 19222
	minImageBytes   = 32 << 20
	slirpHost       = "10.0.2.2"
	defaultVKey     = "vk-roundpen-internal"
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
	SandboxID   string      `json:"sandbox_id"`
	PID         int         `json:"pid"`
	Image       string      `json:"image"`
	Disk        string      `json:"disk"`
	VNCSock     string      `json:"vnc_sock"`
	CDPHostPort int         `json:"cdp_host_port"`
	SSHHostPort int         `json:"ssh_host_port,omitempty"`
	Ports       map[int]int `json:"ports"` // guest -> host
	MemoryMB    int         `json:"memory_mb,omitempty"`
	CPUs        int         `json:"cpus,omitempty"`
	Kernel      string      `json:"kernel,omitempty"`
	Initrd      string      `json:"initrd,omitempty"`
	Append      string      `json:"append,omitempty"`
	EnvFile     string      `json:"env_file,omitempty"`
	cmd         *exec.Cmd   `json:"-"`
}

type bootConfig struct {
	Kernel string `json:"kernel"`
	Initrd string `json:"initrd"`
	Append string `json:"append"`
}

// New returns a QEMU backend. dataRoot is the Roundpen data directory.
func New(dataRoot string) (*Backend, error) {
	if dataRoot == "" {
		return nil, fmt.Errorf("qemu: data root is required")
	}
	bin := os.Getenv("ROUNDPEN_QEMU_BIN")
	if bin == "" {
		bin = "qemu-system-x86_64"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("qemu: %s not found on PATH (install qemu-system-x86)", bin)
	}
	if _, err := exec.LookPath("qemu-img"); err != nil {
		return nil, fmt.Errorf("qemu: qemu-img not found on PATH (install qemu-utils)")
	}
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

func resolveImage(img string) (string, error) {
	if img == "" {
		return "", fmt.Errorf("qemu: qcow2 image path required")
	}
	if !filepath.IsAbs(img) {
		abs, err := filepath.Abs(img)
		if err != nil {
			return "", fmt.Errorf("qemu: image %q: %w", img, err)
		}
		img = abs
	}
	if _, err := os.Stat(img); err != nil {
		return "", fmt.Errorf("qemu: image %q: %w (build with images/browser-qemu/build.sh or images/agent-qemu/build.sh)", img, err)
	}
	return img, nil
}

func checkImage(img string) error {
	dir := filepath.Dir(img)
	if _, err := os.Stat(filepath.Join(dir, "BUILD_INCOMPLETE.txt")); err == nil {
		return fmt.Errorf("qemu: %s is a placeholder; run images/browser-qemu/build.sh or images/agent-qemu/build.sh", img)
	}
	st, err := os.Stat(img)
	if err != nil {
		return fmt.Errorf("qemu: image %q: %w", img, err)
	}
	if st.Size() < minImageBytes {
		return fmt.Errorf("qemu: image %s is too small (%d bytes); rebuild with images/*/build.sh", img, st.Size())
	}
	if _, err := loadBootConfig(img); err != nil {
		return err
	}
	return nil
}

func loadBootConfig(img string) (*bootConfig, error) {
	dir := filepath.Dir(img)
	cfg := &bootConfig{
		Append: "root=/dev/vda rw console=tty0 console=ttyS0 systemd.unit=graphical.target",
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "boot.json")); err == nil {
		var file bootConfig
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("qemu: boot.json: %w", err)
		}
		if file.Kernel != "" {
			cfg.Kernel = file.Kernel
		}
		if file.Initrd != "" {
			cfg.Initrd = file.Initrd
		}
		if file.Append != "" {
			cfg.Append = file.Append
		}
	}
	if cfg.Kernel == "" {
		cfg.Kernel = "vmlinuz"
	}
	if cfg.Initrd == "" {
		cfg.Initrd = "initrd.img"
	}
	kernel := cfg.Kernel
	initrd := cfg.Initrd
	if !filepath.IsAbs(kernel) {
		kernel = filepath.Join(dir, kernel)
	}
	if !filepath.IsAbs(initrd) {
		initrd = filepath.Join(dir, initrd)
	}
	if _, err := os.Stat(kernel); err != nil {
		return nil, fmt.Errorf("qemu: kernel sidecar %s missing; rebuild with images/*/build.sh", kernel)
	}
	if _, err := os.Stat(initrd); err != nil {
		return nil, fmt.Errorf("qemu: initrd sidecar %s missing; rebuild with images/*/build.sh", initrd)
	}
	cfg.Kernel = kernel
	cfg.Initrd = initrd
	return cfg, nil
}

func memoryMBFromOpts(opts backend.CreateOpts) int {
	if opts.MemoryLimit <= 0 {
		return defaultMemoryMB
	}
	mb := int(opts.MemoryLimit / (1024 * 1024))
	if mb < 512 {
		return 512
	}
	return mb
}

func cpusFromOpts(opts backend.CreateOpts) int {
	if opts.CPULimit < 1 {
		return defaultCPUs
	}
	n := int(opts.CPULimit)
	if n < 1 {
		return 1
	}
	return n
}

func kvmAvailable() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

// rewriteGuestURL rewrites control-plane loopback URLs to the slirp host
// so the guest can reach roundpend / llmgw.
func rewriteGuestURL(v string) string {
	r := strings.NewReplacer(
		"http://127.0.0.1", "http://"+slirpHost,
		"https://127.0.0.1", "https://"+slirpHost,
		"http://localhost", "http://"+slirpHost,
		"https://localhost", "https://"+slirpHost,
		"http://[::1]", "http://"+slirpHost,
		"https://[::1]", "https://"+slirpHost,
	)
	return r.Replace(v)
}

func defaultGatewayBase() string {
	for _, k := range []string{"ROUNDPEN_LLMGW_PUBLIC_URL", "ROUNDPEN_PREVIEW_PUBLIC_URL"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	return "http://127.0.0.1:9527"
}

func mergeGuestEnv(opts backend.CreateOpts) map[string]string {
	env := make(map[string]string, len(opts.Env)+8)
	for k, v := range opts.Env {
		env[k] = v
	}
	base := strings.TrimRight(env["ROUNDPEN_URL"], "/")
	if base == "" {
		base = defaultGatewayBase()
		env["ROUNDPEN_URL"] = base
	}
	if env["ANTHROPIC_BASE_URL"] == "" {
		env["ANTHROPIC_BASE_URL"] = base + "/llmgw/anthropic"
	}
	if env["OPENAI_BASE_URL"] == "" {
		env["OPENAI_BASE_URL"] = base + "/llmgw/openai"
	}
	key := env["ANTHROPIC_API_KEY"]
	if key == "" {
		key = env["OPENAI_API_KEY"]
	}
	if key == "" {
		key = defaultVKey
	}
	if env["ANTHROPIC_API_KEY"] == "" {
		env["ANTHROPIC_API_KEY"] = key
	}
	if env["OPENAI_API_KEY"] == "" {
		env["OPENAI_API_KEY"] = key
	}
	if env["ANTHROPIC_AUTH_TOKEN"] == "" {
		env["ANTHROPIC_AUTH_TOKEN"] = key
	}
	if env["ACP_PERMISSION_MODE"] == "" {
		env["ACP_PERMISSION_MODE"] = "bypassPermissions"
	}
	for k, v := range env {
		env[k] = rewriteGuestURL(v)
	}
	return env
}

func writeGuestEnv(path string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for k := range env {
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# generated by roundpen qemu backend — do not edit\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, env[k])
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func qemuArgs(v *vm, kvm bool) []string {
	mem := v.MemoryMB
	if mem <= 0 {
		mem = defaultMemoryMB
	}
	cpus := v.CPUs
	if cpus <= 0 {
		cpus = defaultCPUs
	}
	fwds := []string{fmt.Sprintf("hostfwd=tcp:127.0.0.1:%d-:%d", v.CDPHostPort, cdpGuestPort)}
	if v.SSHHostPort > 0 {
		fwds = append(fwds, fmt.Sprintf("hostfwd=tcp:127.0.0.1:%d-:%d", v.SSHHostPort, sshGuestPort))
	}
	hostfwd := strings.Join(fwds, ",")
	machine := "q35,accel=kvm:tcg"
	cpu := "host"
	if !kvm {
		machine = "q35,accel=tcg"
		cpu = "max"
	}
	args := []string{
		"-name", "roundpen-" + v.SandboxID,
		"-machine", machine,
		"-cpu", cpu,
		"-smp", strconv.Itoa(cpus),
		"-m", strconv.Itoa(mem),
		"-drive", fmt.Sprintf("file=%s,if=virtio,cache=writeback", v.Disk),
		"-netdev", "user,id=net0," + hostfwd,
		"-device", "virtio-net-pci,netdev=net0",
		"-vga", "virtio",
		"-display", "none",
		"-vnc", "unix:" + v.VNCSock,
		"-serial", "file:" + filepath.Join(filepath.Dir(v.VNCSock), "serial.log"),
		"-D", filepath.Join(filepath.Dir(v.VNCSock), "qemu.log"),
		"-daemonize",
		"-pidfile", filepath.Join(filepath.Dir(v.VNCSock), "qemu.pid"),
	}
	if v.Kernel != "" {
		args = append(args, "-kernel", v.Kernel)
		if v.Initrd != "" {
			args = append(args, "-initrd", v.Initrd)
		}
		appendCmd := v.Append
		if appendCmd == "" {
			appendCmd = "root=/dev/vda rw console=tty0 console=ttyS0 systemd.unit=graphical.target"
		}
		args = append(args, "-append", appendCmd)
	}
	if v.EnvFile != "" {
		args = append(args, "-fw_cfg", "name=opt/roundpen/env,file="+v.EnvFile)
	}
	return args
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

	b.mu.Lock()
	defer b.mu.Unlock()
	cdpHost := b.allocPort()
	sshHost := b.allocPort()
	v := &vm{
		SandboxID:   opts.SandboxID,
		Image:       img,
		Disk:        disk,
		VNCSock:     filepath.Join(dir, "vnc.sock"),
		CDPHostPort: cdpHost,
		SSHHostPort: sshHost,
		Ports:       map[int]int{cdpGuestPort: cdpHost, sshGuestPort: sshHost},
		MemoryMB:    memoryMBFromOpts(opts),
		CPUs:        cpusFromOpts(opts),
		Kernel:      boot.Kernel,
		Initrd:      boot.Initrd,
		Append:      boot.Append,
		EnvFile:     envFile,
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
	return b.save(v)
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
	_ = ctx
	_ = sandboxID
	_ = opts
	return nil, fmt.Errorf("qemu: Exec not supported yet (use CDP/desktop)")
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
