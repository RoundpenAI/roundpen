package template

import "strings"

// NormalizeSlot returns agent|browser|mobile (default agent).
func NormalizeSlot(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "browser":
		return "browser"
	case "mobile":
		return "mobile"
	default:
		return "agent"
	}
}

// SlotFromProfile maps legacy profile values onto slots.
func SlotFromProfile(profile string) string {
	if strings.EqualFold(strings.TrimSpace(profile), "browser") {
		return "browser"
	}
	return "agent"
}
