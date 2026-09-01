package builder_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

func TestSanitizeImageRef(t *testing.T) {
	if got := builder.SanitizeImageRef("registry.local/roundpen/"); got != "registry.local/roundpen" {
		t.Fatalf("got %q", got)
	}
	if got := builder.JoinImageRef("registry.local/roundpen", "tpl:latest"); got != "registry.local/roundpen/tpl:latest" {
		t.Fatalf("got %q", got)
	}
}

func TestTemplateImageTags(t *testing.T) {
	tags := builder.TemplateImageTags("python", "deadbeef-id")
	if len(tags) != 2 || tags[0] != "python:deadbeef-id" || tags[1] != "python:latest" {
		t.Fatalf("tags=%v", tags)
	}
	tags = builder.TemplateImageTags("python", "deadbeef-id", "v2", "latest", "default")
	if len(tags) != 3 || tags[2] != "python:v2" {
		t.Fatalf("version tags=%v", tags)
	}
}

func TestWriteBuildContext(t *testing.T) {
	dir, cleanup, err := builder.WriteBuildContext("FROM alpine:3.20\n")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "FROM alpine") {
		t.Fatalf("dockerfile=%q", data)
	}
}

func TestDirContextURI(t *testing.T) {
	got := builder.DirContextURI("/tmp/ctx")
	if got != "dir:///tmp/ctx" {
		t.Fatalf("got %q", got)
	}
}
