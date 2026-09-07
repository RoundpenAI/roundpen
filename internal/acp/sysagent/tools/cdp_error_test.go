package tools

import (
	"errors"
	"strings"
	"testing"
)

func TestWrapBrowserEnsure_stableAndNonRetryable(t *testing.T) {
	err := WrapBrowserEnsure(errors.New(
		`env cdp (nothing listening on guest :9222): cdp attach: failed to modify wsURL: Get "http://127.0.0.1:40495/json/version": read tcp 127.0.0.1:50616->127.0.0.1:40495: read: connection reset by peer`,
	))
	var infra *InfraError
	if !errors.As(err, &infra) || infra.Kind != "cdp_unavailable" {
		t.Fatalf("got %#v", err)
	}
	out := FormatToolError(err)
	if strings.Contains(out, "40495") || strings.Contains(out, "50616") {
		t.Fatalf("ephemeral ports leaked: %s", out)
	}
	if InfraKind(out) != "cdp_unavailable" {
		t.Fatalf("kind from formatted: %q", InfraKind(out))
	}
	if WrapBrowserEnsure(errors.New("unknown ref")) != nil &&
		WrapBrowserEnsure(errors.New("unknown ref")).Error() != "unknown ref" {
		t.Fatalf("page errors should pass through: %v", WrapBrowserEnsure(errors.New("unknown ref")))
	}
}
