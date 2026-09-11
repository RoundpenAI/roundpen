package policy

import "testing"

func TestCheckNetwork_DevSites(t *testing.T) {
	p := &Profile{NetworkTier: NetworkDevSites}
	d := CheckNetwork(p, "https://github.com/foo/bar")
	if !d.Allowed {
		t.Fatalf("github should allow: %+v", d)
	}
	d = CheckNetwork(p, "https://evil.example")
	if d.Allowed || !d.Appliable {
		t.Fatalf("evil should soft-deny appliable: %+v", d)
	}
}

func TestCheckCapability(t *testing.T) {
	p := &Profile{}
	d := CheckCapability(p, "browser")
	if d.Allowed || !d.Appliable {
		t.Fatalf("%+v", d)
	}
	p.Capabilities.Browser = true
	if !CheckCapability(p, "browser").Allowed {
		t.Fatal("expected allow")
	}
}

func TestCheckDirectory(t *testing.T) {
	p := &Profile{}
	if !CheckDirectory(p, "/workspace/x", "read").Allowed {
		t.Fatal("workspace")
	}
	d := CheckDirectory(p, "/home/mike/secret", "read")
	if d.Allowed || !d.Appliable {
		t.Fatalf("%+v", d)
	}
	p.DirectoryGrants = []DirGrant{{Path: "/home/mike/dev", Mode: "read"}}
	if !CheckDirectory(p, "/home/mike/dev/roundpen", "read").Allowed {
		t.Fatal("granted read")
	}
	d = CheckDirectory(p, "/home/mike/dev/x", "readwrite")
	if d.Allowed {
		t.Fatalf("write should deny on read grant: %+v", d)
	}
}
