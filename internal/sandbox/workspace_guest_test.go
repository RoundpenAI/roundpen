package sandbox

import "testing"

func TestGuestRelRejectsEscape(t *testing.T) {
	if _, err := guestRel(".."); err == nil {
		t.Fatal("expected error")
	}
	if _, err := guestRel("../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := guestRel("foo/../../etc"); err == nil {
		t.Fatal("expected error")
	}
	got, err := guestRel("docs/a.txt")
	if err != nil || got != "docs/a.txt" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = guestRel(".")
	if err != nil || got != "." {
		t.Fatalf("root: %q %v", got, err)
	}
	got, err = guestRel("")
	if err != nil || got != "." {
		t.Fatalf("empty: %q %v", got, err)
	}
}
