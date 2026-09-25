package settingitems

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
)

// LegacyProxy is an admin-defined proxy profile from the pre-item settings
// document.
type LegacyProxy struct {
	ID          string
	Name        string
	URL         string
	Description string
}

// LegacyUserProxy is a per-user proxy selection from the pre-item user columns.
type LegacyUserProxy struct {
	Username string
	Slot     string
	ItemID   string
}

// LegacySearch is the pre-item web search configuration.
type LegacySearch struct {
	Endpoint string
	APIKey   string
	ProxyURL string
}

// LegacyBrowser is the pre-item CDP configuration.
type LegacyBrowser struct {
	Provider string
	Endpoint string
	Token    string
	Port     int
}

// LegacyInput carries pre-item configuration across for import at boot.
type LegacyInput struct {
	Proxies     []LegacyProxy
	UserProxies []LegacyUserProxy
	// RawProxyURLs are single-value proxy URLs (llmgw upstreams, web search)
	// that become proxy items so items referencing them keep working.
	RawProxyURLs []string
	Search       *LegacySearch
	Browser      *LegacyBrowser
	LLM          *LegacyLLM
}

// LegacyImport seeds items and bindings from pre-item configuration. It is
// idempotent per kind: a kind that already has items is left alone, so the
// import can gain kinds in later releases without re-importing earlier ones.
// Proxy items synthesized for single-value URLs are looked up by their derived
// id, so those keep resolving even on a database that already has items.
// Rows that fail normalization or validation are skipped with a warning rather
// than blocking startup.
func LegacyImport(ctx context.Context, cat *Catalog, in LegacyInput) error {
	proxyIDs, err := cat.importProxies(ctx, in)
	if err != nil {
		return err
	}
	if err := cat.importSearch(ctx, in, proxyIDs); err != nil {
		return err
	}
	if err := cat.importBrowser(ctx, in); err != nil {
		return err
	}
	if err := cat.importLLM(ctx, in, proxyIDs); err != nil {
		return err
	}
	return cat.Reload(ctx)
}

// importBrowser turns a non-default CDP configuration into a browser item and
// binds it as the global default, so an upgraded install keeps probing the
// source it had. A default install (auto, no endpoint) stays item-free and
// resolves straight from the process config.
func (c *Catalog) importBrowser(ctx context.Context, in LegacyInput) error {
	def, ok := c.reg.Kind(KindBrowser)
	if !ok || in.Browser == nil {
		return nil
	}
	legacy := *in.Browser
	if legacy.Provider == "" {
		legacy.Provider = "auto"
	}
	if legacy.Provider == "auto" && legacy.Endpoint == "" && legacy.Token == "" && legacy.Port <= 0 {
		return nil
	}
	existing, err := c.store.List(ctx, KindBrowser)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	id := "managed"
	if legacy.Provider != "auto" && legacy.Provider != "docker" {
		id = legacy.Provider
	}
	it := Item{
		Kind:    KindBrowser,
		ID:      id,
		Name:    "Browser (" + legacy.Provider + ")",
		Enabled: true,
		Config: map[string]any{
			"provider": legacy.Provider,
			"endpoint": legacy.Endpoint,
			"token":    legacy.Token,
			"port":     legacy.Port,
		},
	}
	if err := NormalizeItem(def, &it); err != nil {
		return err
	}
	it = MergeSecrets(def, it, Item{})
	if err := ValidateItem(def, it); err != nil {
		slog.Warn("settingitems: skipping legacy browser config", "err", err)
		return nil
	}
	if err := c.store.Upsert(ctx, it); err != nil {
		return err
	}
	slot := slotForKindDefault(KindBrowser)
	if slot == "" {
		return nil
	}
	return c.store.SetBinding(ctx, Binding{
		Scope: GlobalScope, Slot: slot, Kind: KindBrowser, ItemID: it.ID,
	})
}

// importProxies imports the legacy profile list and synthesizes items for the
// single-value proxy URLs. It returns a url → item id map for later kinds.
func (c *Catalog) importProxies(ctx context.Context, in LegacyInput) (map[string]string, error) {
	def, ok := c.reg.Kind(KindProxy)
	if !ok {
		return nil, nil
	}
	byURL := map[string]string{}
	existing, err := c.store.List(ctx, KindProxy)
	if err != nil {
		return nil, err
	}
	for _, it := range existing {
		if u := it.Secret("url"); u != "" {
			byURL[u] = it.ID
		}
	}
	if len(existing) == 0 {
		remap := map[string]string{}
		for i, p := range in.Proxies {
			if _, dup := remap[p.ID]; dup {
				continue
			}
			it := Item{
				Kind:        KindProxy,
				ID:          p.ID,
				Name:        p.Name,
				Description: p.Description,
				Enabled:     true,
				Position:    i,
				Config:      map[string]any{"url": p.URL},
			}
			if err := NormalizeItem(def, &it); err != nil {
				slog.Warn("settingitems: skipping proxy profile", "id", p.ID, "err", err)
				continue
			}
			it = MergeSecrets(def, it, Item{})
			if err := ValidateItem(def, it); err != nil {
				slog.Warn("settingitems: skipping proxy profile", "id", p.ID, "err", err)
				continue
			}
			if err := c.store.Upsert(ctx, it); err != nil {
				return nil, err
			}
			remap[p.ID] = it.ID
			if u := it.Secret("url"); u != "" {
				byURL[u] = it.ID
			}
		}
		for _, up := range in.UserProxies {
			id, ok := remap[up.ItemID]
			if !ok {
				continue
			}
			if _, ok := c.reg.Slot(up.Slot); !ok {
				continue
			}
			binding := Binding{Scope: UserScope(up.Username), Slot: up.Slot, Kind: KindProxy, ItemID: id}
			if err := c.store.SetBinding(ctx, binding); err != nil {
				return nil, err
			}
		}
	}
	for _, raw := range in.RawProxyURLs {
		id, err := c.synthesizeProxy(ctx, def, raw, byURL)
		if err != nil {
			return nil, err
		}
		if id != "" {
			byURL[strings.TrimSpace(raw)] = id
		}
	}
	return byURL, nil
}

// synthesizeProxy creates a proxy item for a single-value URL the first time
// it is seen and returns its id. The id derives from the URL, so later boots
// find the same item instead of duplicating it.
func (c *Catalog) synthesizeProxy(ctx context.Context, def KindDef, raw string, known map[string]string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if id, ok := known[raw]; ok {
		return id, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		slog.Warn("settingitems: skipping invalid legacy proxy url", "url", raw)
		return "", nil
	}
	id := SlugifyID("egress-" + u.Host)
	if id == "" {
		return "", nil
	}
	it := Item{
		Kind:    KindProxy,
		ID:      id,
		Name:    "Egress " + u.Host,
		Enabled: true,
		Config:  map[string]any{"url": raw},
	}
	if err := NormalizeItem(def, &it); err != nil {
		return "", err
	}
	it = MergeSecrets(def, it, Item{})
	if err := ValidateItem(def, it); err != nil {
		slog.Warn("settingitems: skipping invalid legacy proxy url", "url", raw, "err", err)
		return "", nil
	}
	if _, err := c.store.Get(ctx, KindProxy, it.ID); err == nil {
		return it.ID, nil
	}
	if err := c.store.Upsert(ctx, it); err != nil {
		return "", err
	}
	return it.ID, nil
}

func (c *Catalog) importSearch(ctx context.Context, in LegacyInput, proxyIDs map[string]string) error {
	def, ok := c.reg.Kind(KindSearch)
	if !ok || in.Search == nil {
		return nil
	}
	existing, err := c.store.List(ctx, KindSearch)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	cfg := map[string]any{"endpoint": in.Search.Endpoint, "apiKey": in.Search.APIKey}
	if id := proxyIDs[strings.TrimSpace(in.Search.ProxyURL)]; id != "" {
		cfg["proxyId"] = id
	}
	it := Item{
		Kind:    KindSearch,
		ID:      "default",
		Name:    "Web search",
		Enabled: true,
		Config:  cfg,
	}
	if err := NormalizeItem(def, &it); err != nil {
		return err
	}
	it = MergeSecrets(def, it, Item{})
	if err := ValidateItem(def, it); err != nil {
		slog.Warn("settingitems: skipping legacy web search", "err", err)
		return nil
	}
	if err := c.store.Upsert(ctx, it); err != nil {
		return err
	}
	// An implicit binding reproduces today's behavior: the configured search
	// backend is the global default for the slot.
	slot := slotForKindDefault(def.Kind)
	if slot == "" {
		return nil
	}
	return c.store.SetBinding(ctx, Binding{
		Scope:  GlobalScope,
		Slot:   slot,
		Kind:   KindSearch,
		ItemID: it.ID,
	})
}

// slotForKindDefault returns the slot a freshly imported kind should bind as
// the global default, so migrated configuration stays effective.
func slotForKindDefault(kind Kind) string {
	switch kind {
	case KindSearch:
		return SlotSearchDefault
	case KindBrowser:
		return SlotBrowserDefault
	case KindLLM:
		return SlotLLMDefault
	default:
		return ""
	}
}

// LegacyLLM is the pre-item LLM gateway configuration.
type LegacyLLM struct {
	OpenAI       LegacyUpstream
	Anthropic    LegacyUpstream
	DefaultModel string
	// EmbeddingAlias is the client-facing alias seeded into the OpenAI
	// provider's model map (llmgw.EmbeddingModelAlias).
	EmbeddingAlias string
	EmbeddingModel string
}

// LegacyUpstream is one pre-item upstream endpoint.
type LegacyUpstream struct {
	BaseURL  string
	APIKey   string
	ProxyURL string
}

// importLLM turns the two hardcoded upstreams into items and binds every
// pre-item consumer slot so an upgraded install keeps behaving the same.
func (c *Catalog) importLLM(ctx context.Context, in LegacyInput, proxyIDs map[string]string) error {
	def, ok := c.reg.Kind(KindLLM)
	if !ok || in.LLM == nil {
		return nil
	}
	existing, err := c.store.List(ctx, KindLLM)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	specs := []struct {
		id       string
		protocol string
		up       LegacyUpstream
	}{
		{"openai", "openai", in.LLM.OpenAI},
		{"anthropic", "anthropic", in.LLM.Anthropic},
	}
	created := map[string]bool{}
	for _, spec := range specs {
		if spec.up.BaseURL == "" && spec.up.APIKey == "" {
			continue
		}
		if spec.up.BaseURL == "" || spec.up.APIKey == "" {
			slog.Warn("settingitems: skipping incomplete legacy upstream", "id", spec.id)
			continue
		}
		it := Item{
			Kind:    KindLLM,
			ID:      spec.id,
			Name:    spec.id,
			Enabled: true,
			Config: map[string]any{
				"protocol":     spec.protocol,
				"baseUrl":      spec.up.BaseURL,
				"apiKey":       spec.up.APIKey,
				"defaultModel": in.LLM.DefaultModel,
			},
		}
		if spec.id == "openai" && in.LLM.EmbeddingAlias != "" && in.LLM.EmbeddingModel != "" {
			it.Config["modelMap"] = map[string]any{in.LLM.EmbeddingAlias: in.LLM.EmbeddingModel}
		}
		if id := proxyIDs[strings.TrimSpace(spec.up.ProxyURL)]; id != "" {
			it.Config["proxyId"] = id
		}
		if err := NormalizeItem(def, &it); err != nil {
			slog.Warn("settingitems: skipping legacy upstream", "id", spec.id, "err", err)
			continue
		}
		it = MergeSecrets(def, it, Item{})
		if err := ValidateItem(def, it); err != nil {
			slog.Warn("settingitems: skipping legacy upstream", "id", spec.id, "err", err)
			continue
		}
		if err := c.store.Upsert(ctx, it); err != nil {
			return err
		}
		created[spec.id] = true
	}
	if len(created) == 0 {
		return nil
	}
	defaultID := "openai"
	if !created["openai"] && created["anthropic"] {
		defaultID = "anthropic"
	}
	// The sandbox agent keeps the Anthropic endpoint when one exists, matching
	// the pre-item env (Claude Code speaks the Anthropic dialect).
	agentID := defaultID
	if created["anthropic"] {
		agentID = "anthropic"
	}
	bindings := []struct {
		slot  string
		id    string
		model string
	}{
		{SlotLLMDefault, defaultID, in.LLM.DefaultModel},
		{SlotLLMAgent, agentID, in.LLM.DefaultModel},
		{SlotLLMSysAgent, defaultID, in.LLM.DefaultModel},
		{SlotLLMClassifier, defaultID, in.LLM.DefaultModel},
		{SlotLLMPlanner, defaultID, in.LLM.DefaultModel},
	}
	if created["openai"] {
		bindings = append(bindings, struct {
			slot  string
			id    string
			model string
		}{SlotLLMEmbedding, "openai", in.LLM.EmbeddingModel})
	}
	for _, b := range bindings {
		if _, ok := c.reg.Slot(b.slot); !ok {
			continue
		}
		binding := Binding{Scope: GlobalScope, Slot: b.slot, Kind: KindLLM, ItemID: b.id}
		if model := strings.TrimSpace(b.model); model != "" {
			binding.Params = map[string]any{"model": model}
		}
		if err := c.store.SetBinding(ctx, binding); err != nil {
			return err
		}
	}
	return nil
}
