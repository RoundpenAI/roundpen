package hostsetup

import "testing"

func TestDeterministicPlanDockerAndBrowser(t *testing.T) {
	ctx := WizardContext{Preset: "code_browser"}
	f := HostFacts{DockerReady: false, BinariesOK: false, BrowserImageOK: false}
	p := DeterministicPlan(ctx, f, PrivilegeManual)
	ids := actionIDs(p)
	if !contains(ids, ActionInstallDocker) || !contains(ids, ActionInstallQEMU) || !contains(ids, ActionBuildBrowserImage) {
		t.Fatalf("ids=%v", ids)
	}
	for _, a := range p.Actions {
		if a.ID == ActionInstallDocker && a.Privilege != PrivilegeManual {
			t.Fatalf("docker privilege: %s", a.Privilege)
		}
	}
}

func TestDeterministicPlanEmptyWhenReady(t *testing.T) {
	f := HostFacts{DockerReady: true}
	p := DeterministicPlan(WizardContext{Preset: "code"}, f, PrivilegeAuto)
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
