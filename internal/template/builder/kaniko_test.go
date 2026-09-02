package builder_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

func TestKaniko_Build_withFakeExecutor(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "executor")
	script := `#!/bin/sh
echo "kaniko fake build $*"
exit 0
`
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	k, err := builder.NewKaniko(builder.KanikoConfig{
		Executor:          exe,
		DestinationPrefix: "registry.test/roundpen",
		RegistryMirrors:   []string{"docker.1ms.run"},
		NoSandbox:         true, // fake shell script; skip bwrap
	})
	if err != nil {
		t.Fatal(err)
	}

	var lines []string
	artifact, snapshot, err := k.Build(context.Background(), "alpine:3.20", builder.Spec{
		Steps: []builder.Step{{Type: "RUN", Args: []string{"true"}}},
	}, []string{"myapp:deadbeef", "myapp:latest"}, func(_, _, msg string) {
		lines = append(lines, msg)
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifact != "registry.test/roundpen/myapp:deadbeef" {
		t.Fatalf("artifact=%q", artifact)
	}
	if snapshot {
		t.Fatal("kaniko builder should not produce snapshots")
	}
	if len(lines) == 0 {
		t.Fatal("expected log lines")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "--registry-mirror=docker.1ms.run") {
		t.Fatalf("expected registry-mirror in args, got:\n%s", joined)
	}
	if !strings.Contains(joined, "--destination=registry.test/roundpen/myapp:deadbeef") {
		t.Fatalf("expected build destination, got:\n%s", joined)
	}
	if !strings.Contains(joined, "--destination=registry.test/roundpen/myapp:latest") {
		t.Fatalf("expected latest destination, got:\n%s", joined)
	}
}

func TestKaniko_requiresDestination(t *testing.T) {
	_, err := builder.NewKaniko(builder.KanikoConfig{})
	if err == nil || !strings.Contains(err.Error(), "destination") {
		t.Fatalf("err=%v", err)
	}
}

func TestKaniko_requiresExecutorOnPath(t *testing.T) {
	_, err := builder.NewKaniko(builder.KanikoConfig{
		Executor:          "roundpen-kaniko-missing-" + t.Name(),
		DestinationPrefix: "registry.test/roundpen",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
