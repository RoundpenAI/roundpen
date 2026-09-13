package template

import (
	"strings"
	"testing"
)

func TestIsBuiltin(t *testing.T) {
	if !IsBuiltin(DefaultNamespace, "code-agent", "system") {
		t.Fatal("code-agent should be builtin")
	}
	if IsBuiltin(DefaultNamespace, "my-agent", "") {
		t.Fatal("user template should not be builtin")
	}
	if IsBuiltin(DefaultNamespace, "code-agent", "") {
		t.Fatal("code-agent without system creator is not protected as builtin")
	}
}

func TestBuiltinBrowserSeedUsesOCI(t *testing.T) {
	entries := builtinEntries("docker", "")
	var found bool
	for _, e := range entries {
		if e.Name != "browser" {
			continue
		}
		found = true
		if e.Slot != "browser" {
			t.Fatalf("slot = %q, want browser", e.Slot)
		}
		if strings.HasSuffix(e.ArtifactRef, ".qcow2") {
			t.Fatalf("artifact = %q must be an OCI image", e.ArtifactRef)
		}
		if !strings.Contains(e.ArtifactRef, "browserless/chrome") {
			t.Fatalf("artifact = %q, want browserless/chrome", e.ArtifactRef)
		}
	}
	if !found {
		t.Fatal("builtin entry \"browser\" not found")
	}
	if _, ok := BuiltinNames["browser-desktop"]; ok {
		t.Fatal("browser-desktop must be dropped from BuiltinNames")
	}
}
