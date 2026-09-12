package hostsetup

import (
	"context"
	"testing"
	"time"
)

func TestCreatePlanEmptyWhenReady(t *testing.T) {
	svc := &Service{
		Facts: func() HostFacts {
			return HostFacts{BinariesOK: true, AgentImageOK: true, BrowserImageOK: true}
		},
	}
	rec, err := svc.CreatePlan(context.Background(), "u", WizardContext{Preset: "code"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Actions) != 0 {
		t.Fatalf("want empty plan, got %#v", rec.Actions)
	}
}

func TestSensitiveManualStaysPendingManual(t *testing.T) {
	svc := &Service{
		Facts: func() HostFacts {
			return HostFacts{BinariesOK: false, AgentImageOK: true, BrowserImageOK: true}
		},
		Privilege: func() Privilege { return PrivilegeManual },
	}
	rec, err := svc.CreatePlan(context.Background(), "u", WizardContext{Preset: "code"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Actions) != 1 || rec.Actions[0].ActionID != ActionInstallQEMU {
		t.Fatalf("%#v", rec.Actions)
	}
	if rec.Actions[0].Status != StatusPendingManual {
		t.Fatalf("status=%s", rec.Actions[0].Status)
	}
}

func TestSensitiveAutoConfirmRuns(t *testing.T) {
	svc := &Service{
		Facts: func() HostFacts {
			return HostFacts{BinariesOK: false, AgentImageOK: true, BrowserImageOK: true}
		},
		Privilege: func() Privilege { return PrivilegeAuto },
		Runner:    NewRunner(RunnerConfig{RepoRoot: t.TempDir()}),
	}
	rec, err := svc.CreatePlan(context.Background(), "u", WizardContext{Preset: "code"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Actions[0].Status != StatusPendingConfirm {
		t.Fatalf("status=%s", rec.Actions[0].Status)
	}
	if _, err := svc.Confirm("u", rec.ID, ActionInstallQEMU); err != nil {
		t.Fatal(err)
	}
	var st string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := svc.Get("u", rec.ID)
		if err != nil {
			t.Fatal(err)
		}
		st = got.Actions[0].Status
		if st == StatusRunning || st == StatusFailed || st == StatusSucceeded {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st != StatusRunning && st != StatusFailed && st != StatusSucceeded && st != StatusQueued {
		t.Fatalf("unexpected status after confirm: %s", st)
	}
}

func TestRecheckSucceedsWhenFactOK(t *testing.T) {
	ok := false
	svc := &Service{
		Facts: func() HostFacts {
			return HostFacts{BinariesOK: ok, AgentImageOK: true, BrowserImageOK: true}
		},
		Privilege: func() Privilege { return PrivilegeManual },
	}
	rec, err := svc.CreatePlan(context.Background(), "u", WizardContext{Preset: "code"})
	if err != nil {
		t.Fatal(err)
	}
	ok = true
	rec2, err := svc.Recheck("u", rec.ID, ActionInstallQEMU)
	if err != nil {
		t.Fatal(err)
	}
	if rec2.Actions[0].Status != StatusSucceeded {
		t.Fatalf("status=%s", rec2.Actions[0].Status)
	}
}
