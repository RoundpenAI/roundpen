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

func TestPickReject(t *testing.T) {
	rejectOnce := acp.PermissionOption{OptionId: "no", Kind: acp.PermissionOptionKindRejectOnce}
	rejectAlways := acp.PermissionOption{OptionId: "deny_all", Kind: acp.PermissionOptionKindRejectAlways}
	custom := acp.PermissionOption{OptionId: "reject", Kind: ""}
	allow := acp.PermissionOption{OptionId: "allow", Kind: acp.PermissionOptionKindAllowOnce}

	if got := PickReject([]acp.PermissionOption{allow, rejectAlways, rejectOnce}); got != "no" {
		t.Fatalf("prefer reject-once, got %q", got)
	}
	if got := PickReject([]acp.PermissionOption{allow, custom, rejectAlways}); got != "reject" {
		t.Fatalf("prefer plain reject id over session-scoped kind, got %q", got)
	}
	if got := PickReject([]acp.PermissionOption{allow, rejectAlways}); got != "deny_all" {
		t.Fatalf("fall back to reject-always kind, got %q", got)
	}
	if got := PickReject([]acp.PermissionOption{allow}); got != "" {
		t.Fatalf("no reject option, got %q", got)
	}
	if got := PickReject(nil); got != "" {
		t.Fatalf("empty opts, got %q", got)
	}
}
