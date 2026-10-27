// Package buildinfo exposes the Roundpen release version stamped at link time.
package buildinfo

import (
	"os"
	"strings"
)

// Version is set at link time:
//
//	-ldflags "-X github.com/RoundpenAI/roundpen/internal/buildinfo.Version=0.4.7"
//
// Unset builds (go run / go test) report "dev".
var Version = "dev"

// Current returns ROUNDPEN_VERSION when set, otherwise the link-time Version.
func Current() string {
	if v := strings.TrimSpace(os.Getenv("ROUNDPEN_VERSION")); v != "" {
		return v
	}
	if v := strings.TrimSpace(Version); v != "" {
		return v
	}
	return "dev"
}
