package workspace

import "testing"

func TestUserWorkspaceID(t *testing.T) {
	if got := UserWorkspaceID("Admin"); got != "user-admin" {
		t.Fatalf("got %q", got)
	}
	if got := UserWorkspaceID("Mike.Chen"); got != "user-mike-chen" {
		t.Fatalf("got %q", got)
	}
	if got := UserWorkspaceID("  "); got != "user-user" {
		t.Fatalf("empty: %q", got)
	}
}

func TestAgentSandboxID(t *testing.T) {
	if got := AgentSandboxID("Alice"); got != "alice" {
		t.Fatalf("got %q", got)
	}
	if got := AgentSandboxID("Mike.Chen"); got != "mike-chen" {
		t.Fatalf("got %q", got)
	}
	if "roundpen-"+AgentSandboxID("bob") != "roundpen-bob" {
		t.Fatal("docker name contract")
	}
}
