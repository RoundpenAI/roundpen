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
