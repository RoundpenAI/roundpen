package automode

import "testing"

func TestExpandSplicesDefaults(t *testing.T) {
	got := Expand(Rules{Allow: []string{"Custom rule A", DefaultsToken}})
	def := Defaults()
	if len(got.Allow) != len(def.Allow)+1 {
		t.Fatalf("allow len = %d, want %d", len(got.Allow), len(def.Allow)+1)
	}
	if got.Allow[0] != "Custom rule A" {
		t.Fatalf("first entry = %q", got.Allow[0])
	}
	if got.Allow[1] != def.Allow[0] {
		t.Fatalf("defaults not spliced after custom entry: %q", got.Allow[1])
	}
	if len(got.Environment) != len(def.Environment) || len(got.SoftDeny) != len(def.SoftDeny) {
		t.Fatal("empty lists should fall back to defaults")
	}
}

func TestExpandSplicesDefaultsInMiddle(t *testing.T) {
	got := Expand(Rules{SoftDeny: []string{"before", DefaultsToken, "after"}})
	def := Defaults()
	if len(got.SoftDeny) != len(def.SoftDeny)+2 {
		t.Fatalf("len = %d", len(got.SoftDeny))
	}
	if got.SoftDeny[0] != "before" || got.SoftDeny[1] != def.SoftDeny[0] {
		t.Fatalf("splice position wrong: %v", got.SoftDeny[:2])
	}
	if got.SoftDeny[len(got.SoftDeny)-1] != "after" {
		t.Fatalf("trailing entry lost")
	}
}

func TestExpandReplacesWithoutToken(t *testing.T) {
	got := Expand(Rules{HardDeny: []string{"Only mine"}})
	if len(got.HardDeny) != 1 || got.HardDeny[0] != "Only mine" {
		t.Fatalf("expected replacement, got %v", got.HardDeny)
	}
}

func TestDefaultsNonEmpty(t *testing.T) {
	def := Defaults()
	if len(def.Environment) == 0 || len(def.Allow) == 0 || len(def.SoftDeny) == 0 || len(def.HardDeny) == 0 {
		t.Fatal("built-in defaults must cover all four lists")
	}
}
