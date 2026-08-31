package builder_test

import (
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

func TestDockerfile(t *testing.T) {
	spec := builder.Spec{
		Steps: []builder.Step{
			{Type: "RUN", Args: []string{"echo hello"}},
			{Type: "WORKDIR", Args: []string{"/app"}},
		},
		StartCmd: "python -m http.server 8000",
		ReadyCmd: "waitForPort(8000)",
	}
	df, err := builder.Dockerfile("ubuntu:22.04", spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(df, "FROM ubuntu:22.04") {
		t.Fatalf("missing FROM: %s", df)
	}
	if !strings.Contains(df, "RUN echo hello") {
		t.Fatalf("missing RUN: %s", df)
	}
	if !strings.Contains(df, "/roundpen/init.sh") {
		t.Fatalf("missing init script: %s", df)
	}
}

func TestCacheKeyStable(t *testing.T) {
	spec := builder.Spec{
		FromImage: "alpine:3.20",
		Steps:     []builder.Step{{Type: "RUN", Args: []string{"true"}}},
	}
	a, err := builder.CacheKey(spec)
	if err != nil {
		t.Fatal(err)
	}
	b, err := builder.CacheKey(spec)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("cache keys differ: %q vs %q", a, b)
	}
	spec.Force = true
	c, err := builder.CacheKey(spec)
	if err != nil {
		t.Fatal(err)
	}
	if a != c {
		t.Fatal("force flag should not affect cache key normalization")
	}
}

func TestReadyShell(t *testing.T) {
	got := builder.ReadyShell("waitForPort(8080)")
	if !strings.Contains(got, "8080") {
		t.Fatalf("unexpected probe: %q", got)
	}
}
