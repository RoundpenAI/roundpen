package sysagent

import "testing"

func TestResolveAskOptionLabels(t *testing.T) {
	labels := []string{"Alpha", "Beta", "Gamma", ""}

	got, err := resolveAskOptionLabels("opt-1", labels)
	if err != nil || got != "Beta" {
		t.Fatalf("single: got %q err=%v", got, err)
	}

	got, err = resolveAskOptionLabels("opt-0,opt-2", labels)
	if err != nil || got != "Alpha, Gamma" {
		t.Fatalf("multi: got %q err=%v", got, err)
	}

	got, err = resolveAskOptionLabels("opt-0, other", labels)
	if err != nil || got != "Alpha, Other (see user reply)" {
		t.Fatalf("other: got %q err=%v", got, err)
	}

	if _, err := resolveAskOptionLabels("opt-9", labels); err == nil {
		t.Fatal("expected unknown option error")
	}
	if _, err := resolveAskOptionLabels("", labels); err == nil {
		t.Fatal("expected empty answer error")
	}
}
