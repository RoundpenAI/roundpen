package agentapi

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestPermissionOptionLabel(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionId: "opt-0", Name: "用代理继续"},
		{OptionId: "opt-1", Name: "直连"},
		{OptionId: "allow_once", Name: "允许一次"},
	}
	if got := permissionOptionLabel(opts, "opt-0"); got != "用代理继续" {
		t.Fatalf("got %q", got)
	}
	if got := permissionOptionLabel(opts, "allow_once"); got != "允许一次" {
		t.Fatalf("got %q", got)
	}
	if got := permissionOptionLabel(opts, "missing"); got != "missing" {
		t.Fatalf("fallback got %q", got)
	}
	if got := permissionOptionLabel(nil, "opt-0"); got != "opt-0" {
		t.Fatalf("nil options got %q", got)
	}
}
