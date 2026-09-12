package multi

import "testing"

func TestRouteKind(t *testing.T) {
	tests := []struct {
		slot, image, want string
	}{
		{"browser", "alpine:3.20", "qemu"},
		{"mobile", "", "qemu"},
		{"agent", "images/browser-qemu/out/browser.qcow2", "qemu"},
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
