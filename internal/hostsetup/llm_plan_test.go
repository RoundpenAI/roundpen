package hostsetup

import "testing"

func TestReconcilePlanDropsUnknown(t *testing.T) {
	raw := Plan{
		Summary: "x",
		Actions: []PlannedAction{
			{ID: "rm_rf", Title: "bad"},
			{ID: ActionInstallDocker, Title: "装 Docker"},
		},
	}
	out := Reconcile(raw, HostFacts{DockerReady: false}, PrivilegeAuto, WizardContext{Preset: "code"})
	if len(out.Actions) != 1 || out.Actions[0].ID != ActionInstallDocker {
		t.Fatalf("%#v", out.Actions)
	}
	if out.Actions[0].Command == "" {
		t.Fatal("command required from catalog")
	}
}
