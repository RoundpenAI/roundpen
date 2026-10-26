package buildinfo

import "testing"

func TestCurrentFallsBackToVersion(t *testing.T) {
	t.Setenv("ROUNDPEN_VERSION", "")
	Version = "0.4.7"
	if got := Current(); got != "0.4.7" {
		t.Fatalf("Current() = %q, want 0.4.7", got)
	}
}

func TestCurrentPrefersEnv(t *testing.T) {
	t.Setenv("ROUNDPEN_VERSION", "9.9.9")
	Version = "0.4.7"
	if got := Current(); got != "9.9.9" {
		t.Fatalf("Current() = %q, want 9.9.9", got)
	}
}
