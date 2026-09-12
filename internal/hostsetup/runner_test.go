package hostsetup

import (
	"context"
	"io"
	"testing"
)

func TestRunnerRejectsUnknownAction(t *testing.T) {
	r := NewRunner(RunnerConfig{RepoRoot: t.TempDir()})
	err := r.Run(context.Background(), "nope", PrivilegeAuto, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstallQEMUArgvRoot(t *testing.T) {
	argv := installQEMUArgv(true)
	want := []string{"apt-get", "install", "-y", "qemu-system-x86", "qemu-utils"}
	if len(argv) != len(want) {
		t.Fatalf("got %v", argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("got %v", argv)
		}
	}
}

func TestInstallQEMUArgvSudoN(t *testing.T) {
	argv := installQEMUArgv(false)
	if argv[0] != "sudo" || argv[1] != "-n" {
		t.Fatalf("got %v", argv)
	}
}
