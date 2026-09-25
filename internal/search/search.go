// Package search resolves the configured web-search backend for a usage site.
package search

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

// Config is a resolved search backend.
type Config struct {
	Endpoint string
	APIKey   string
	// ProxyURL is the egress proxy of the search call; the web tools reuse it
	// for WebFetch and skill installs.
	ProxyURL string
}

// Kind returns the search kind definition.
func Kind() settingitems.KindDef {
	return settingitems.KindDef{
		Kind: settingitems.KindSearch,
		Name: "Web search",
		Fields: []settingitems.Field{
			{
				Key:   "endpoint",
				Type:  settingitems.FieldURL,
				Label: "Endpoint",
				Hint:  "Tavily-compatible search API. Empty uses https://api.tavily.com.",
			},
			{
				Key:   "apiKey",
				Type:  settingitems.FieldSecret,
				Label: "API key",
				Hint:  "Sent as a bearer token.",
			},
			{
				Key:     "proxyId",
				Type:    settingitems.FieldItemRef,
				RefKind: settingitems.KindProxy,
				Label:   "Egress proxy",
				Hint:    "Used for the search call, WebFetch and skill installs.",
			},
		},
		Selectable: []string{},
		Validate:   validate,
	}
}

func validate(it *settingitems.Item) error {
	endpoint := it.String("endpoint")
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("endpoint must be an http(s) URL")
	}
	return nil
}

// Slots returns the search usage sites.
func Slots() []settingitems.SlotDef {
	return []settingitems.SlotDef{{
		Key:          settingitems.SlotSearchDefault,
		Kind:         settingitems.KindSearch,
		Name:         "Web search",
		Description:  "Backend of the WebSearch tool; its proxy also routes WebFetch and skill installs.",
		UserOverride: true,
	}}
}

// Resolve returns the search backend for a slot and user. An unresolved slot
// yields a zero Config, which keeps WebSearch unregistered (today's behavior
// when no endpoint or key is configured).
func Resolve(snap *settingitems.Snapshot, slot, user string) Config {
	res := snap.Resolve(slot, user)
	if !res.OK {
		return Config{}
	}
	return Config{
		Endpoint: strings.TrimSpace(res.Item.String("endpoint")),
		APIKey:   res.Item.Secret("apiKey"),
		ProxyURL: ProxyURL(snap, res.Item.String("proxyId")),
	}
}

// ProxyURL resolves a proxy item id to its URL ("" = direct).
func ProxyURL(snap *settingitems.Snapshot, itemID string) string {
	if itemID == "" {
		return ""
	}
	it, ok := snap.Item(settingitems.KindProxy, itemID)
	if !ok {
		return ""
	}
	return it.Secret("url")
}
