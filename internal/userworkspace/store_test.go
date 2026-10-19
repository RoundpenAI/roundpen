package userworkspace

import "testing"

func TestAcceptPath(t *testing.T) {
	if _, err := AcceptPath("/vol1/@appshare/roundpen"); err == nil {
		t.Fatal("appshare should be rejected")
	}
	if _, err := AcceptPath("/vol1/@appdata/roundpen/data"); err == nil {
		t.Fatal("appdata should be rejected")
	}
	if _, err := AcceptPath("relative"); err == nil {
		t.Fatal("relative should be rejected")
	}
	got, err := AcceptPath("/vol1/1000/Roundpen")
	if err != nil || got != "/vol1/1000/Roundpen" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestFnosUID(t *testing.T) {
	if _, ok := FnosUID("local", "fnos-1000"); ok {
		t.Fatal("local account")
	}
	uid, ok := FnosUID("fnos", "fnos-1000")
	if !ok || uid != 1000 {
		t.Fatalf("uid %d %v", uid, ok)
	}
}

func TestCovers(t *testing.T) {
	if !Covers([]string{"/vol1/1000"}, "/vol1/1000/Roundpen") {
		t.Fatal("expected cover")
	}
	if Covers([]string{"/vol1/1000/other"}, "/vol1/1000/Roundpen") {
		t.Fatal("unexpected cover")
	}
}
