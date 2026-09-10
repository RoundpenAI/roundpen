package multi

import "testing"

func TestRouteKind(t *testing.T) {
	tests := []struct {
		slot, image, engine, def, want string
	}{
		{"browser", "alpine:3.20", "", "docker", "qemu"},
		{"mobile", "", "", "kern", "qemu"},
		{"agent", "images/agent-qemu/out/agent.qcow2", "docker", "docker", "qemu"},
		{"agent", "roundpen-code-agent:local", "docker", "qemu", "docker"},
		{"agent", "host", "kern", "qemu", "kern"},
		{"agent", "python:3.12-slim", "qemu", "qemu", "docker-or-kern"},
		{"agent", "python:3.12-slim", "", "qemu", "docker-or-kern"},
		{"agent", "python:3.12-slim", "", "docker", "docker"},
	}
	for _, tc := range tests {
		got := routeKind(tc.slot, tc.image, tc.engine, tc.def)
		if got != tc.want {
			t.Fatalf("routeKind(%q,%q,%q,%q)=%s want %s", tc.slot, tc.image, tc.engine, tc.def, got, tc.want)
		}
	}
}
