package tools_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func TestResolveWorkspacePath(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"/workspace/a.go", "a.go", true},
		{"/workspace", ".", true},
		{"a/b", "a/b", true},
		{"./x", "x", true},
		{"/etc/passwd", "", false},
		{"/workspace/../etc/passwd", "", false},
		{"../x", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, err := tools.ResolveWorkspacePath(tc.in)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("%q: got %q err=%v want %q", tc.in, got, err, tc.want)
			}
		} else if err == nil {
			t.Fatalf("%q: expected error, got %q", tc.in, got)
		}
	}
}

func TestResolveWorkspacePathFromProjectCwd(t *testing.T) {
	base := "/workspace/projects/demo"
	got, err := tools.ResolveWorkspacePathFrom(base, "README.md")
	if err != nil || got != "projects/demo/README.md" {
		t.Fatalf("rel: got %q err=%v", got, err)
	}
	got, err = tools.ResolveWorkspacePathFrom(base, "/workspace/AGENTS.md")
	if err != nil || got != "AGENTS.md" {
		t.Fatalf("abs: got %q err=%v", got, err)
	}
}

func TestAgentBinderDefaultCwd(t *testing.T) {
	b := &tools.AgentBinder{Cwd: "/workspace/projects/demo"}
	if got := b.DefaultCwd(); got != "/workspace/projects/demo" {
		t.Fatalf("DefaultCwd=%q", got)
	}
	if got := (&tools.AgentBinder{}).DefaultCwd(); got != tools.WorkspaceRoot {
		t.Fatalf("empty DefaultCwd=%q", got)
	}
}
