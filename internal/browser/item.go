package browser

import (
	"fmt"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

// Source is the CDP settings a browser slot resolves to (provider / endpoint /
// token / port). It is not a Chromium user-data profile — that is Profile.
// A Source is resolved into a Runtime at Session attach time.
type Source struct {
	ItemID   string
	Provider string // auto | docker | host | remote | cloud
	Endpoint string
	Token    string
	Port     int
}

// EffectiveProvider maps "auto" to the managed container, the same way the
// instance-wide config resolves it.
func (p Source) EffectiveProvider() string {
	provider := strings.ToLower(strings.TrimSpace(p.Provider))
	if provider == "" || provider == config.CDPProviderAuto {
		return config.CDPProviderDocker
	}
	return provider
}

// DefaultSource is the process-config browser source, used for env-seeded
// installs and whenever no item is selected.
func DefaultSource(cfg *config.Config) Source {
	if cfg == nil {
		return Source{Provider: config.CDPProviderAuto, Port: config.DefaultCDPPort}
	}
	return Source{
		Provider: cfg.CDP.Provider,
		Endpoint: cfg.CDP.Endpoint,
		Token:    cfg.CDP.Token,
		Port:     cfg.CDP.Port,
	}
}

// SourceFromItem projects a browser item onto a Source.
func SourceFromItem(it settingitems.Item) Source {
	port := it.Int("port")
	if port <= 0 {
		port = config.DefaultCDPPort
	}
	return Source{
		ItemID:   it.ID,
		Provider: it.String("provider"),
		Endpoint: it.String("endpoint"),
		Token:    it.Secret("token"),
		Port:     port,
	}
}

// Resolve returns the browser source for a slot and user; an unset or
// unresolvable slot keeps the supplied fallback (the config-derived default).
func Resolve(snap *settingitems.Snapshot, slot, user string, fallback Source) Source {
	if snap == nil {
		return fallback
	}
	res := snap.Resolve(slot, user)
	if !res.OK {
		return fallback
	}
	return SourceFromItem(res.Item)
}

// Kind returns the browser kind definition.
func Kind() settingitems.KindDef {
	return settingitems.KindDef{
		Kind: settingitems.KindBrowser,
		Name: "Browser source",
		Fields: []settingitems.Field{
			{
				Key:      "provider",
				Type:     settingitems.FieldEnum,
				Label:    "Provider",
				Required: true,
				// Settings UI offers docker (local install) or remote (URL).
				// auto/host/cloud remain valid for legacy items and env config.
				Options: []string{config.CDPProviderDocker, config.CDPProviderRemote},
				Hint:    "Local Docker installs a managed browserless container; remote dials an external browserless URL.",
			},
			{
				Key:   "endpoint",
				Type:  settingitems.FieldString,
				Label: "Browserless URL",
				Hint:  "Required for remote (ws://… or http://… of a browserless deployment).",
			},
			{
				Key:      "token",
				Type:     settingitems.FieldSecret,
				Label:    "Endpoint token",
				Hint:     "Optional bearer token for remote.",
				Advanced: true,
			},
			{
				Key:      "port",
				Type:     settingitems.FieldInt,
				Label:    "CDP port",
				Hint:     "Guest port of the managed container (default 3000).",
				Advanced: true,
			},
		},
		Selectable: []string{},
		Normalize:  normalizeItem,
		Validate:   validateItem,
	}
}

func normalizeItem(it *settingitems.Item) error {
	provider := strings.TrimSpace(it.String("provider"))
	if provider == "" || provider == config.CDPProviderAuto {
		it.Config["provider"] = config.CDPProviderDocker
	}
	if it.Int("port") <= 0 {
		it.Config["port"] = config.DefaultCDPPort
	}
	return nil
}

func validateItem(it *settingitems.Item) error {
	cdp := config.CDPConfig{
		Provider: it.String("provider"),
		Endpoint: it.String("endpoint"),
		Token:    it.Secret("token"),
		Port:     it.Int("port"),
	}
	if err := config.NormalizeCDP(&cdp); err != nil {
		return fmt.Errorf("browser item: %w", err)
	}
	return nil
}

// Slots returns the browser usage sites.
func Slots() []settingitems.SlotDef {
	return []settingitems.SlotDef{{
		Key:          settingitems.SlotBrowserDefault,
		Kind:         settingitems.KindBrowser,
		Name:         "Browser source",
		Description:  "CDP source the browser sandbox and the live view attach to.",
		UserOverride: true,
	}}
}
