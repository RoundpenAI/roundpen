package qemu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

func TestMemoryAndCPUFromOpts(t *testing.T) {
	if memoryMBFromOpts(backend.CreateOpts{}) != defaultMemoryMB {
		t.Fatalf("default memory")
	}
	if memoryMBFromOpts(backend.CreateOpts{MemoryLimit: 100}) != 512 {
		t.Fatalf("floor 512")
	}
	if memoryMBFromOpts(backend.CreateOpts{MemoryLimit: 4 << 30}) != 4096 {
		t.Fatalf("4GiB → 4096, got %d", memoryMBFromOpts(backend.CreateOpts{MemoryLimit: 4 << 30}))
	}
	if cpusFromOpts(backend.CreateOpts{}) != defaultCPUs {
		t.Fatalf("default cpus")
	}
	if cpusFromOpts(backend.CreateOpts{CPULimit: 4}) != 4 {
		t.Fatalf("4 cpus")
	}
}

func TestCheckImageRejectsPlaceholder(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "browser.qcow2")
	if err := os.WriteFile(img, make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BUILD_INCOMPLETE.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := checkImage(img)
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckImageRejectsTinyAndMissingKernel(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "browser.qcow2")
	if err := os.WriteFile(img, make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	err := checkImage(img)
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("tiny: %v", err)
	}
	if err := os.WriteFile(img, make([]byte, minImageBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	err = checkImage(img)
	if err == nil || !strings.Contains(err.Error(), "kernel sidecar") {
		t.Fatalf("missing kernel: %v", err)
	}
}

func TestLoadBootConfig(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "browser.qcow2")
	if err := os.WriteFile(img, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vmlinuz"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "initrd.img"), []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "boot.json"), []byte(`{"append":"root=/dev/vda rw"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadBootConfig(img)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kernel != filepath.Join(dir, "vmlinuz") {
		t.Fatalf("kernel %s", cfg.Kernel)
	}
	if cfg.Append != "root=/dev/vda rw" {
		t.Fatalf("append %s", cfg.Append)
	}
}

func TestQemuArgsKernelBoot(t *testing.T) {
	v := &vm{
		SandboxID:   "sb1",
		Disk:        "/tmp/disk.qcow2",
		VNCSock:     "/tmp/qemu/sb1/vnc.sock",
		CDPHostPort: 19222,
		MemoryMB:    4096,
		CPUs:        2,
		Kernel:      "/img/vmlinuz",
		Initrd:      "/img/initrd.img",
		Append:      "root=/dev/vda rw",
	}
	args := qemuArgs(v, true)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-kernel /img/vmlinuz",
		"-initrd /img/initrd.img",
		"-append root=/dev/vda rw",
		"-vga virtio",
		"-vnc unix:/tmp/qemu/sb1/vnc.sock",
		"hostfwd=tcp:127.0.0.1:19222-:9222",
		"-m 4096",
		"accel=kvm:tcg",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	noKVM := strings.Join(qemuArgs(v, false), " ")
	if !strings.Contains(noKVM, "accel=tcg") || strings.Contains(noKVM, "accel=kvm:tcg") {
		t.Fatalf("tcg args: %s", noKVM)
	}
}

func TestResolveImageAbs(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "browser.qcow2")
	if err := os.WriteFile(img, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveImage(img)
	if err != nil {
		t.Fatal(err)
	}
	if got != img {
		t.Fatalf("got %s", got)
	}
	_, err = resolveImage(filepath.Join(dir, "missing.qcow2"))
	if err == nil || !strings.Contains(err.Error(), "build.sh") {
		t.Fatalf("missing: %v", err)
	}
}
