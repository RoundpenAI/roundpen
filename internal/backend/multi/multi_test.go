package multi

import "testing"

func TestRouteKind(t *testing.T) {
	tests := []struct {
		slot, image, want string
	}{
		{"browser", "ghcr.io/browserless/chrome:v2.56.7", "docker"},
		{"mobile", "", "qemu"},
		{"agent", "images/agent-qemu/out/agent.qcow2", "qemu"},
		{"agent", "roundpen-code-agent:local", "docker"},
		{"agent", "python:3.12-slim", "docker"},
		{"", "python:3.12-slim", "docker"},
	}
	for _, tc := range tests {
		got := routeKind(tc.slot, tc.image)
		if got != tc.want {
			t.Fatalf("routeKind(%q,%q)=%s want %s", tc.slot, tc.image, got, tc.want)
		}
	}
}
