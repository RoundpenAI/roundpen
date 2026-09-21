package userenv

import (
	"github.com/RoundpenAI/roundpen/internal/settingitems"
	"github.com/RoundpenAI/roundpen/internal/settings"
)

// ProxyKind is the kind definition for egress proxy items. The URL is a
// secret field: it may embed credentials, so it is sealed at rest, masked in
// admin responses, and never exposed to non-admins.
func ProxyKind() settingitems.KindDef {
	return settingitems.KindDef{
		Kind: settingitems.KindProxy,
		Name: "Egress proxy",
		Fields: []settingitems.Field{{
			Key:      "url",
			Type:     settingitems.FieldSecret,
			Label:    "Proxy URL",
			Hint:     "http, https, socks5 or socks5h. May embed credentials.",
			Required: true,
		}},
		Selectable: []string{},
		Validate: func(it *settingitems.Item) error {
			return settings.ValidateProxyURL(it.Secret("url"))
		},
	}
}

// ProxySlots returns the egress proxy usage sites, one per sandbox slot.
func ProxySlots() []settingitems.SlotDef {
	return []settingitems.SlotDef{
		{
			Key:          settingitems.SlotProxyAgent,
			Kind:         settingitems.KindProxy,
			Name:         "Agent egress",
			Description:  "Proxy used for the agent sandbox's outbound traffic.",
			UserOverride: true,
			RebuildsEnv:  true,
		},
		{
			Key:          settingitems.SlotProxyBrowser,
			Kind:         settingitems.KindProxy,
			Name:         "Browser egress",
			Description:  "Proxy used for the browser sandbox's outbound traffic.",
			UserOverride: true,
			RebuildsEnv:  true,
		},
	}
}

// ProxySlotKey maps a sandbox slot ("agent" | "browser") to its proxy slot key.
func ProxySlotKey(slot string) string {
	if slot == SlotBrowser {
		return settingitems.SlotProxyBrowser
	}
	return settingitems.SlotProxyAgent
}

// ProxyURL resolves the egress proxy URL for a slot and user ("" = direct).
func ProxyURL(snap *settingitems.Snapshot, slot, user string) string {
	res := snap.Resolve(slot, user)
	if !res.OK {
		return ""
	}
	return res.Item.Secret("url")
}
