package hostsetup

import "testing"

func TestReconcilePlanDropsUnknown(t *testing.T) {
	raw := Plan{
		Summary: "x",
		Actions: []PlannedAction{
			{ID: "rm_rf", Title: "bad"},
			{ID: ActionBuildAgentImage, Title: "盘"},
		},
	}
	out := Reconcile(raw, HostFacts{BinariesOK: true, AgentImageOK: false}, PrivilegeAuto, WizardContext{Preset: "code"})
	if len(out.Actions) != 1 || out.Actions[0].ID != ActionBuildAgentImage {
		t.Fatalf("%#v", out.Actions)
	}
	if out.Actions[0].Command == "" {
		t.Fatal("command required from catalog")
	}
}
