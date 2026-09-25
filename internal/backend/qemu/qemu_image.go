package qemu

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type bootConfig struct {
	Kernel string `json:"kernel"`
	Initrd string `json:"initrd"`
	Append string `json:"append"`
}

func qemuSystemBin() string {
	bin := os.Getenv("ROUNDPEN_QEMU_BIN")
	if bin == "" {
		return "qemu-system-x86_64"
	}
	return bin
}

// BinariesAvailable reports whether qemu-system and qemu-img are on PATH.
func BinariesAvailable() error {
	bin := qemuSystemBin()
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("qemu: %s not found on PATH (install qemu-system-x86)", bin)
	}
	if _, err := exec.LookPath("qemu-img"); err != nil {
		return fmt.Errorf("qemu: qemu-img not found on PATH (install qemu-utils)")
	}
	return nil
}

// ValidateImage checks that a qcow2 plus kernel sidecars are built and bootable.
func ValidateImage(img string) error {
	p, err := resolveImage(img)
	if err != nil {
		return err
	}
	return checkImage(p)
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
		return "", fmt.Errorf("qemu: image %q: %w (build the qcow2 artifact first)", img, err)
	}
	return img, nil
}

func checkImage(img string) error {
	dir := filepath.Dir(img)
	if _, err := os.Stat(filepath.Join(dir, "BUILD_INCOMPLETE.txt")); err == nil {
		return fmt.Errorf("qemu: %s is a placeholder; build the qcow2 artifact first", img)
	}
	st, err := os.Stat(img)
	if err != nil {
		return fmt.Errorf("qemu: image %q: %w", img, err)
	}
	if st.Size() < minImageBytes {
		return fmt.Errorf("qemu: image %s is too small (%d bytes); rebuild the qcow2 artifact", img, st.Size())
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
		return nil, fmt.Errorf("qemu: kernel sidecar %s missing; rebuild the qcow2 artifact", kernel)
	}
	if _, err := os.Stat(initrd); err != nil {
		return nil, fmt.Errorf("qemu: initrd sidecar %s missing; rebuild the qcow2 artifact", initrd)
	}
	cfg.Kernel = kernel
	cfg.Initrd = initrd
	return cfg, nil
}
