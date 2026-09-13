package docker

import "testing"

func TestRefreshChanged(t *testing.T) {
	cases := []struct {
		name     string
		local    string
		pulled   string
		hadLocal bool
		want     bool
	}{
		{"unchanged digest", "sha256:a", "sha256:a", true, false},
		{"changed digest", "sha256:a", "sha256:b", true, true},
		{"no local image", "", "sha256:a", false, true},
		{"unreadable pulled digest", "sha256:a", "", true, true},
	}
	for _, c := range cases {
		if got := refreshChanged(c.local, c.pulled, c.hadLocal); got != c.want {
			t.Fatalf("%s: refreshChanged(%q,%q,%v)=%v want %v", c.name, c.local, c.pulled, c.hadLocal, got, c.want)
		}
	}
}
