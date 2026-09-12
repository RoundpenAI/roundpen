package hostsetup

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/runtime"
)

func TestDeterministicPlanAgentAndBrowser(t *testing.T) {
	ctx := WizardContext{Preset: "code_browser"}
	snap := runtime.Snapshot{
		Engines: []runtime.EngineStatus{{
			ID:      runtime.EngineQEMU,
			Missing: []string{"QEMU binaries (qemu-system-x86_64, qemu-img)", "agent qcow2 image", "browser qcow2 image"},
		}},
	}
	p := DeterministicPlan(ctx, snap, PrivilegeManual)
	ids := actionIDs(p)
	if !contains(ids, ActionInstallQEMU) || !contains(ids, ActionBuildAgentImage) || !contains(ids, ActionBuildBrowserImage) {
		t.Fatalf("ids=%v", ids)
	}
	for _, a := range p.Actions {
		if a.ID == ActionInstallQEMU && a.Privilege != PrivilegeManual {
			t.Fatalf("qemu privilege: %s", a.Privilege)
		}
	}
}

func TestDeterministicPlanEmptyWhenReady(t *testing.T) {
	snap := runtime.Snapshot{
		Engines: []runtime.EngineStatus{{
			ID:         runtime.EngineQEMU,
			AgentReady: true,
		}},
	}
	p := DeterministicPlan(WizardContext{Preset: "code"}, snap, PrivilegeAuto)
	if len(p.Actions) != 0 {
		t.Fatalf("want empty, got %#v", p.Actions)
	}
}

func actionIDs(p Plan) []string {
	out := make([]string, 0, len(p.Actions))
	for _, a := range p.Actions {
		out = append(out, a.ID)
	}
	return out
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
