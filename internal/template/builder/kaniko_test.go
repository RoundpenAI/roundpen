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
	})
	if err != nil {
		t.Fatal(err)
	}

	var lines []string
	artifact, snapshot, err := k.Build(context.Background(), "alpine:3.20", builder.Spec{
		Steps: []builder.Step{{Type: "RUN", Args: []string{"true"}}},
	}, "template-deadbeef:latest", func(_, _, msg string) {
		lines = append(lines, msg)
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifact != "registry.test/roundpen/template-deadbeef:latest" {
		t.Fatalf("artifact=%q", artifact)
	}
	if snapshot {
		t.Fatal("kaniko builder should not produce snapshots")
	}
	if len(lines) == 0 {
		t.Fatal("expected log lines")
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
