package client

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestPickOrdinaryAllow(t *testing.T) {
	once := acp.PermissionOption{OptionId: "allow_once", Kind: acp.PermissionOptionKindAllowOnce}
	always := acp.PermissionOption{OptionId: "allow_all", Kind: acp.PermissionOptionKindAllowAlways}
	tool := acp.PermissionOption{OptionId: "allow_tool", Kind: acp.PermissionOptionKindAllowOnce}
	allow := acp.PermissionOption{OptionId: "allow", Kind: ""}
	reject := acp.PermissionOption{OptionId: "reject", Kind: acp.PermissionOptionKindRejectOnce}

	if got := PickOrdinaryAllow([]acp.PermissionOption{always, once, reject}); got != "allow_once" {
		t.Fatalf("prefer allow_once, got %q", got)
	}
	if got := PickOrdinaryAllow([]acp.PermissionOption{always, reject}); got != "" {
		t.Fatalf("do not auto-pick allow_all, got %q", got)
	}
	if got := PickOrdinaryAllow([]acp.PermissionOption{allow, reject}); got != "allow" {
		t.Fatalf("id allow, got %q", got)
	}
	if got := PickOrdinaryAllow([]acp.PermissionOption{tool}); got != "allow_tool" {
		t.Fatalf("allow_tool kind once, got %q", got)
	}
	if got := PickOrdinaryAllow(nil); got != "" {
		t.Fatalf("empty opts, got %q", got)
	}
}
