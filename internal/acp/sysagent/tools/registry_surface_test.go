package tools_test

import (
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func TestWorkspaceToolSurface(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterRoundpen(reg, &tools.RoundpenHTTP{BaseURL: "http://127.0.0.1"})
	binder := &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "x"},
		Exec:  &stubExec{},
		Files: &memFiles{data: map[string][]byte{}},
	}
	tools.RegisterShell(reg, binder)
	tools.RegisterFiles(reg, binder)
	tools.RegisterSearch(reg, binder)

	want := map[string]bool{
		"Bash": true, "Edit": true, "GetSettings": true, "Glob": true, "Grep": true,
		"ListEnvironments": true, "ListSessions": true, "ListTemplates": true,
		"Read": true, "Write": true,
	}
	for _, name := range []string{
		"Bash", "Edit", "GetSettings", "Glob", "Grep",
		"ListEnvironments", "ListSessions", "ListTemplates", "Read", "Write",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("missing tool %q", name)
		}
	}
	for _, tool := range reg.List() {
		lower := strings.ToLower(tool.Name)
		if strings.Contains(lower, "sandbox") || strings.Contains(lower, "ensure") || strings.HasPrefix(lower, "roundpen_") {
			t.Fatalf("unexpected tool name %q", tool.Name)
		}
		if strings.Contains(strings.ToLower(tool.Description), "sandbox") {
			t.Fatalf("%s description mentions sandbox", tool.Name)
		}
		delete(want, tool.Name)
	}
	for name := range want {
		t.Fatalf("missing from list: %q", name)
	}
}
