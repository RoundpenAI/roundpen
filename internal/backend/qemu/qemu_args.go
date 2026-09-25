package qemu

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

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
	if v.WorkspaceDisk != "" {
		args = append(args, "-drive", fmt.Sprintf("file=%s,if=virtio,cache=writeback", v.WorkspaceDisk))
	} else if tag := virtfsSpec(v.Workspace); tag != "" {
		args = append(args, "-virtfs", tag)
	}
	return args
}

func virtfsSpec(hostPath string) string {
	hostPath = strings.TrimSpace(hostPath)
	if hostPath == "" || strings.ContainsAny(hostPath, ",\n\r") {
		return ""
	}
	return fmt.Sprintf("local,path=%s,mount_tag=workspace,security_model=mapped-xattr,id=ws", hostPath)
}

func useVirtioWorkspace(opts backend.CreateOpts) bool {
	slot := strings.ToLower(strings.TrimSpace(opts.Slot))
	if slot == "" {
		slot = strings.ToLower(strings.TrimSpace(opts.Env["ROUNDPEN_SLOT"]))
	}
	return slot != "mobile"
}

func ensureWorkspaceDisk(path string) error {
	if path == "" {
		return fmt.Errorf("qemu: workspace disk path required")
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", path, fmt.Sprintf("%dG", workspaceDiskGB))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img create workspace disk: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
