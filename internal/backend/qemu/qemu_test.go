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

func TestRewriteGuestURL(t *testing.T) {
	got := rewriteGuestURL("http://127.0.0.1:9527/llmgw/anthropic")
	if got != "http://10.0.2.2:9527/llmgw/anthropic" {
		t.Fatalf("loopback: %s", got)
	}
	got = rewriteGuestURL("https://localhost/llmgw/openai")
	if got != "https://10.0.2.2/llmgw/openai" {
		t.Fatalf("localhost: %s", got)
	}
	got = rewriteGuestURL("http://0.0.0.0:19001/llmgw/anthropic")
	if got != "http://10.0.2.2:19001/llmgw/anthropic" {
		t.Fatalf("unspecified: %s", got)
	}
	got = rewriteGuestURL("https://api.anthropic.com")
	if got != "https://api.anthropic.com" {
		t.Fatalf("upstream rewritten: %s", got)
	}
	// Prefix replace used to turn this into http://10.0.2.20.0.0.0:19001.
	got = rewriteGuestURL("http://127.0.0.10.0.0.0:19001/llmgw/anthropic")
	if got != "http://127.0.0.10.0.0.0:19001/llmgw/anthropic" {
		t.Fatalf("must not prefix-replace 127.0.0.1 inside garbage host: %s", got)
	}
}

func TestMergeGuestEnvSanitizesGluedListenAddr(t *testing.T) {
	env := mergeGuestEnv(backend.CreateOpts{Env: map[string]string{
		"ROUNDPEN_URL":       "http://127.0.0.10.0.0.0:19001",
		"ANTHROPIC_BASE_URL": "http://127.0.0.10.0.0.0:19001/llmgw/anthropic",
		"OPENAI_BASE_URL":    "http://0.0.0.0:19001/llmgw/openai",
	}})
	if env["ROUNDPEN_URL"] != "http://10.0.2.2:19001" {
		t.Fatalf("base: %s", env["ROUNDPEN_URL"])
	}
	if env["ANTHROPIC_BASE_URL"] != "http://10.0.2.2:19001/llmgw/anthropic" {
		t.Fatalf("anthropic: %s", env["ANTHROPIC_BASE_URL"])
	}
	if env["OPENAI_BASE_URL"] != "http://10.0.2.2:19001/llmgw/openai" {
		t.Fatalf("openai: %s", env["OPENAI_BASE_URL"])
	}
}

func TestMergeGuestEnvRewritesAndFills(t *testing.T) {
	env := mergeGuestEnv(backend.CreateOpts{Env: map[string]string{
		"ROUNDPEN_URL":  "http://127.0.0.1:19001",
		"ROUNDPEN_SLOT": "agent",
	}})
	if env["ANTHROPIC_BASE_URL"] != "http://10.0.2.2:19001/llmgw/anthropic" {
		t.Fatalf("anthropic: %s", env["ANTHROPIC_BASE_URL"])
	}
	if env["OPENAI_BASE_URL"] != "http://10.0.2.2:19001/llmgw/openai" {
		t.Fatalf("openai: %s", env["OPENAI_BASE_URL"])
	}
	if env["ANTHROPIC_API_KEY"] != defaultVKey || env["ANTHROPIC_AUTH_TOKEN"] != defaultVKey {
		t.Fatalf("vkey: %+v", env)
	}
	if env["ROUNDPEN_SLOT"] != "agent" {
		t.Fatalf("slot: %s", env["ROUNDPEN_SLOT"])
	}
}

func TestMergeGuestEnvFillsClaudeModelAliases(t *testing.T) {
	env := mergeGuestEnv(backend.CreateOpts{Env: map[string]string{
		"ROUNDPEN_URL":    "http://127.0.0.1:19001",
		"ANTHROPIC_MODEL": "nvidia/nemotron-3.5-lightning:free",
	}})
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatalf("opus: %s", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	if env["ANTHROPIC_SMALL_FAST_MODEL"] != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatalf("small: %s", env["ANTHROPIC_SMALL_FAST_MODEL"])
	}
}

func TestWriteGuestEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guest.env")
	if err := writeGuestEnv(path, map[string]string{
		"ANTHROPIC_BASE_URL": "http://10.0.2.2:9527/llmgw/anthropic",
		"ANTHROPIC_API_KEY":  defaultVKey,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "ANTHROPIC_BASE_URL=http://10.0.2.2:9527/llmgw/anthropic") {
		t.Fatalf("body: %s", body)
	}
	if !strings.Contains(body, "ANTHROPIC_API_KEY="+defaultVKey) {
		t.Fatalf("key: %s", body)
	}
}

func TestQemuArgsKernelBoot(t *testing.T) {
	v := &vm{
		SandboxID:   "sb1",
		Disk:        "/tmp/disk.qcow2",
		VNCSock:     "/tmp/qemu/sb1/vnc.sock",
		CDPHostPort: 19222,
		SSHHostPort: 19223,
		MemoryMB:    4096,
		CPUs:        2,
		Kernel:      "/img/vmlinuz",
		Initrd:      "/img/initrd.img",
		Append:      "root=/dev/vda rw",
		EnvFile:     "/tmp/qemu/sb1/guest.env",
		Workspace:   "/data/sandboxes/user-admin/workspace",
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
		"hostfwd=tcp:127.0.0.1:19223-:22",
		"-fw_cfg name=opt/roundpen/env,file=/tmp/qemu/sb1/guest.env",
		"-m 4096",
		"accel=kvm:tcg",
		"-virtfs local,path=/data/sandboxes/user-admin/workspace,mount_tag=workspace,security_model=mapped-xattr,id=ws",
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

func TestAttachShell(t *testing.T) {
	got, err := attachShell(backend.AttachExecOpts{
		Cmd:     []string{"claude-agent-acp"},
		WorkDir: "/workspace",
		Env:     map[string]string{"ACP_PERMISSION_MODE": "bypassPermissions"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		". /etc/roundpen/env",
		"export ACP_PERMISSION_MODE='bypassPermissions'",
		"cd '/workspace' && exec 'claude-agent-acp'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if _, err := attachShell(backend.AttachExecOpts{}); err == nil {
		t.Fatal("empty cmd should fail")
	}
}

func TestPrepareGuestCmd(t *testing.T) {
	for _, want := range []string{
		"nameserver 10.0.2.3",
		`"enabled": False`,
		"bypassPermissions",
	} {
		if !strings.Contains(prepareGuestCmd, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestVirtfsSpec(t *testing.T) {
	if virtfsSpec("") != "" {
		t.Fatal("empty")
	}
	if virtfsSpec("/tmp/a,b") != "" {
		t.Fatal("comma rejected")
	}
	got := virtfsSpec("/data/ws")
	if !strings.Contains(got, "path=/data/ws") || !strings.Contains(got, "mount_tag=workspace") {
		t.Fatalf("got %s", got)
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
