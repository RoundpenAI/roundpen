package browser

import (
	"fmt"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

// Profile is the browser source a slot resolves to.
type Profile struct {
	ItemID   string
	Provider string // auto | docker | host | remote | cloud
	Endpoint string
	Token    string
	Port     int
}

// EffectiveProvider maps "auto" to the managed container, the same way the
// instance-wide config resolves it.
func (p Profile) EffectiveProvider() string {
	provider := strings.ToLower(strings.TrimSpace(p.Provider))
	if provider == "" || provider == config.CDPProviderAuto {
		return config.CDPProviderDocker
	}
	return provider
}

// DefaultProfile is the process-config browser source, used for env-seeded
// installs and whenever no item is selected.
func DefaultProfile(cfg *config.Config) Profile {
	if cfg == nil {
		return Profile{Provider: config.CDPProviderAuto, Port: config.DefaultCDPPort}
	}
	return Profile{
		Provider: cfg.CDP.Provider,
		Endpoint: cfg.CDP.Endpoint,
		Token:    cfg.CDP.Token,
		Port:     cfg.CDP.Port,
	}
}

// ProfileFromItem projects a browser item onto a Profile.
func ProfileFromItem(it settingitems.Item) Profile {
	port := it.Int("port")
	if port <= 0 {
		port = config.DefaultCDPPort
	}
	return Profile{
		ItemID:   it.ID,
		Provider: it.String("provider"),
		Endpoint: it.String("endpoint"),
		Token:    it.Secret("token"),
		Port:     port,
	}
}

// Resolve returns the browser source for a slot and user; an unset or
// unresolvable slot keeps the supplied fallback (the config-derived default).
func Resolve(snap *settingitems.Snapshot, slot, user string, fallback Profile) Profile {
	if snap == nil {
		return fallback
	}
	res := snap.Resolve(slot, user)
	if !res.OK {
		return fallback
	}
	return ProfileFromItem(res.Item)
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
				Options:  []string{config.CDPProviderAuto, config.CDPProviderDocker, config.CDPProviderHost, config.CDPProviderRemote, config.CDPProviderCloud},
				Hint:     "auto/docker use the Roundpen-managed container; host runs local Chrome; remote/cloud dial an external browserless.",
			},
			{
				Key:   "endpoint",
				Type:  settingitems.FieldString,
				Label: "CDP endpoint",
				Hint:  "Required for remote/cloud (ws://… or http://… of a browserless deployment).",
			},
			{
				Key:   "token",
				Type:  settingitems.FieldSecret,
				Label: "Endpoint token",
				Hint:  "Optional bearer token for remote/cloud.",
			},
			{
				Key:   "port",
				Type:  settingitems.FieldInt,
				Label: "CDP port",
				Hint:  "Guest port of the managed container (default 3000).",
			},
		},
		Selectable: []string{},
		Normalize:  normalizeItem,
		Validate:   validateItem,
	}
}

func normalizeItem(it *settingitems.Item) error {
	if strings.TrimSpace(it.String("provider")) == "" {
		it.Config["provider"] = config.CDPProviderAuto
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
