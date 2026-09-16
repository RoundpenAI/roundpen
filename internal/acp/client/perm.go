package client

import acp "github.com/coder/acp-go-sdk"

// PickOrdinaryAllow returns an allow-once / allow option id.
// It never picks allow_always / allow_all. Empty means the UI should prompt.
func PickOrdinaryAllow(opts []acp.PermissionOption) string {
	var once, tool string
	for _, o := range opts {
		id := string(o.OptionId)
		if o.Kind == acp.PermissionOptionKindAllowOnce {
			return id
		}
		switch id {
		case "allow_once", "allow":
			if once == "" {
				once = id
			}
		case "allow_tool":
			if tool == "" {
				tool = id
			}
		}
	}
	if once != "" {
		return once
	}
	return tool
}

// PickReject returns a reject option id, preferring reject-once semantics over
// session-scoped rejections. Empty means the request offers no reject option.
func PickReject(opts []acp.PermissionOption) string {
	var fallback string
	for _, o := range opts {
		id := string(o.OptionId)
		switch o.Kind {
		case acp.PermissionOptionKindRejectOnce:
			return id
		case acp.PermissionOptionKindRejectAlways:
			if fallback == "" {
				fallback = id
			}
			continue
		}
		switch id {
		case "reject", "deny", "reject_once":
			return id
		case "reject_tool":
			if fallback == "" {
				fallback = id
			}
		}
	}
	return fallback
}
