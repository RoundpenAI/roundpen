package settingitems_test

import (
	"context"
	"errors"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/search"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

func testRegistry(t *testing.T) *settingitems.Registry {
	t.Helper()
	reg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{{
			Kind: "widget",
			Name: "Widget",
			Fields: []settingitems.Field{
				{Key: "endpoint", Type: settingitems.FieldURL, Required: true},
				{Key: "protocol", Type: settingitems.FieldEnum, Options: []string{"alpha", "beta"}},
				{Key: "token", Type: settingitems.FieldSecret},
			},
			Selectable: []string{"endpoint"},
		}},
		[]settingitems.SlotDef{
			{Key: "widget.primary", Kind: "widget", Name: "Primary", UserOverride: true},
			{Key: "widget.strict", Kind: "widget", Name: "Strict", Protocols: []string{"beta"}},
			{Key: "widget.child", Kind: "widget", Name: "Child", FallbackSlot: "widget.primary"},
			{Key: "widget.pinned", Kind: "widget", Name: "Pinned", DefaultItem: "alpha"},
		},
	)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return reg
}

func newTestCatalog(t *testing.T) *settingitems.Catalog {
	t.Helper()
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), testRegistry(t))
	if err := cat.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	return cat
}

func saveItem(t *testing.T, cat *settingitems.Catalog, id, name, protocol string, enabled bool) settingitems.Item {
	t.Helper()
	it, err := cat.Save(context.Background(), settingitems.Item{
		Kind: "widget", ID: id, Name: name, Enabled: enabled,
		Config: map[string]any{"endpoint": "https://" + id + ".test", "protocol": protocol},
	})
	if err != nil {
		t.Fatalf("save %s: %v", id, err)
	}
	return it
}

func bind(t *testing.T, cat *settingitems.Catalog, scope, slot, itemID string) {
	t.Helper()
	err := cat.SetBinding(context.Background(), settingitems.Binding{Scope: scope, Slot: slot, ItemID: itemID})
	if err != nil {
		t.Fatalf("bind %s/%s -> %s: %v", scope, slot, itemID, err)
	}
}

func TestResolveChain(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)
	saveItem(t, cat, "alpha", "Alpha", "alpha", true)
	saveItem(t, cat, "beta", "Beta", "beta", true)
	bind(t, cat, settingitems.GlobalScope, "widget.primary", "alpha")
	bind(t, cat, settingitems.UserScope("bob"), "widget.primary", "beta")

	snap := cat.Snapshot()
	if res := snap.Resolve("widget.primary", "bob"); !res.OK || res.Item.ID != "beta" || res.Source != "user" {
		t.Fatalf("user override: %+v", res)
	}
	if res := snap.Resolve("widget.primary", "alice"); !res.OK || res.Item.ID != "alpha" || res.Source != "global" {
		t.Fatalf("global default: %+v", res)
	}
	// A user with no override falls back to the global binding.
	if res := snap.Resolve("widget.primary", ""); !res.OK || res.Item.ID != "alpha" {
		t.Fatalf("anonymous: %+v", res)
	}

	// Unbound slot with a default item.
	if res := snap.Resolve("widget.pinned", ""); !res.OK || res.Item.ID != "alpha" || res.Source != "default" {
		t.Fatalf("default item: %+v", res)
	}

	// Unbound slot falls back through FallbackSlot.
	if res := snap.Resolve("widget.child", "bob"); !res.OK || res.Item.ID != "beta" || res.Source != "fallback" {
		t.Fatalf("fallback slot: %+v", res)
	}

	// Nothing configured at all.
	if res := snap.Resolve("widget.strict", ""); res.OK {
		t.Fatalf("expected unresolved slot, got %+v", res)
	}
	if err := cat.Reload(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestResolveSkipsDisabledAndMismatchedItems(t *testing.T) {
	cat := newTestCatalog(t)
	saveItem(t, cat, "alpha", "Alpha", "alpha", true)
	saveItem(t, cat, "off", "Off", "beta", false)
	bind(t, cat, settingitems.GlobalScope, "widget.primary", "alpha")

	// Disabling an item drops through to the next hop instead of failing.
	if _, err := cat.Save(context.Background(), settingitems.Item{
		Kind: "widget", ID: "alpha", Name: "Alpha", Enabled: false,
		Config: map[string]any{"endpoint": "https://alpha.test", "protocol": "alpha"},
	}); err != nil {
		t.Fatal(err)
	}
	if res := cat.Snapshot().Resolve("widget.primary", ""); res.OK {
		t.Fatalf("disabled item must not resolve: %+v", res)
	}

	// A protocol the slot does not accept never resolves.
	if err := cat.SetBinding(context.Background(), settingitems.Binding{
		Scope: settingitems.GlobalScope, Slot: "widget.strict", ItemID: "off",
	}); err == nil {
		t.Fatal("expected protocol mismatch to be rejected")
	}
}

func TestBindingValidation(t *testing.T) {
	cat := newTestCatalog(t)
	saveItem(t, cat, "beta", "Beta", "beta", true)

	err := cat.SetBinding(context.Background(), settingitems.Binding{Slot: "nope", ItemID: "beta"})
	if !errors.Is(err, settingitems.ErrSlotUnknown) {
		t.Fatalf("unknown slot: %v", err)
	}
	err = cat.SetBinding(context.Background(), settingitems.Binding{
		Scope: settingitems.UserScope("bob"), Slot: "widget.strict", ItemID: "beta",
	})
	if err == nil {
		t.Fatal("expected per-user override on a non-overridable slot to fail")
	}
	err = cat.SetBinding(context.Background(), settingitems.Binding{Slot: "widget.primary", ItemID: "ghost"})
	if err == nil {
		t.Fatal("expected unknown item to fail")
	}
}

func TestDeleteBoundItem(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)
	saveItem(t, cat, "alpha", "Alpha", "alpha", true)
	bind(t, cat, settingitems.GlobalScope, "widget.primary", "alpha")

	err := cat.Delete(ctx, "widget", "alpha", false)
	var bound *settingitems.BoundError
	if !errors.As(err, &bound) || len(bound.Slots) != 1 || bound.Slots[0] != "widget.primary" {
		t.Fatalf("expected bound error, got %v", err)
	}
	if err := cat.Delete(ctx, "widget", "alpha", true); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	if _, ok := cat.Snapshot().Item("widget", "alpha"); ok {
		t.Fatal("item still present after delete")
	}
	if _, ok := cat.Snapshot().Binding(settingitems.GlobalScope, "widget.primary"); ok {
		t.Fatal("binding still present after cascade delete")
	}
}

func TestSecretsMaskedOnViews(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)
	saved, err := cat.Save(ctx, settingitems.Item{
		Kind: "widget", ID: "alpha", Name: "Alpha", Enabled: true,
		Config: map[string]any{"endpoint": "https://alpha.test", "protocol": "alpha", "token": "sk-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Secret("token") != "sk-secret" {
		t.Fatalf("secret not kept: %+v", saved.Secrets)
	}
	def, _ := testRegistry(t).Kind("widget")

	admin := saved.AdminView(def)
	if admin.Config["token"] != "●●●●●●●●" {
		t.Fatalf("admin view must mask secrets: %+v", admin.Config)
	}
	if admin.Secrets != nil {
		t.Fatal("admin view must not carry secrets")
	}
	user := saved.SelectView(def)
	if _, ok := user.Config["token"]; ok {
		t.Fatalf("select view leaked a secret: %+v", user.Config)
	}
	if user.Config["endpoint"] != "https://alpha.test" {
		t.Fatalf("select view dropped a selectable field: %+v", user.Config)
	}

	// Submitting the mask back keeps the stored secret.
	updated, err := cat.Save(ctx, settingitems.Item{
		Kind: "widget", ID: "alpha", Name: "Alpha", Enabled: true,
		Config: map[string]any{"endpoint": "https://alpha.test", "protocol": "alpha", "token": "●●●●●●●●"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Secret("token") != "sk-secret" {
		t.Fatalf("masked submit must keep the secret: %+v", updated.Secrets)
	}
}

func TestLegacyImport(t *testing.T) {
	ctx := context.Background()
	reg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{userenv.ProxyKind()},
		userenv.ProxySlots(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	if err := cat.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	in := settingitems.LegacyInput{
		Proxies: []settingitems.LegacyProxy{
			{ID: "US Egress", Name: "US egress", URL: "socks5://10.0.0.9:1080"},
			{ID: "jp", Name: "JP egress", URL: "http://user:pass@10.0.0.8:8080"},
			// Rows the kind rejects are skipped, not imported.
			{ID: "bad-url", Name: "Bad", URL: "ftp://10.0.0.7:1080"},
		},
		UserProxies: []settingitems.LegacyUserProxy{
			{Username: "bob", Slot: settingitems.SlotProxyAgent, ItemID: "jp"},
			{Username: "bob", Slot: settingitems.SlotProxyBrowser, ItemID: "US Egress"},
		},
	}
	if err := settingitems.LegacyImport(ctx, cat, in); err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := len(cat.Snapshot().Items(settingitems.KindProxy, true)); got != 2 {
		t.Fatalf("expected two valid profiles, got %d", got)
	}
	res := cat.Snapshot().Resolve(settingitems.SlotProxyAgent, "bob")
	if !res.OK || res.Item.ID != "jp" || res.Source != "user" {
		t.Fatalf("imported binding: %+v", res)
	}
	if res.Item.Secret("url") != "http://user:pass@10.0.0.8:8080" {
		t.Fatalf("imported url: %+v", res.Item.Secrets)
	}
	// A profile id that is not a slug is normalized, and selections follow.
	browser := cat.Snapshot().Resolve(settingitems.SlotProxyBrowser, "bob")
	if !browser.OK || browser.Item.ID != "us-egress" {
		t.Fatalf("slug remap: %+v", browser)
	}

	// Re-running the import is a no-op.
	if err := settingitems.LegacyImport(ctx, cat, in); err != nil {
		t.Fatal(err)
	}
	if got := len(cat.Snapshot().Items(settingitems.KindProxy, true)); got != 2 {
		t.Fatalf("import is not idempotent: %d items", got)
	}
}

func TestSlugifyID(t *testing.T) {
	cases := map[string]string{
		"US Egress":   "us-egress",
		"  jp  ":      "jp",
		"gpt-4o mini": "gpt-4o-mini",
		"a/b":         "a-b",
	}
	for in, want := range cases {
		if got := settingitems.SlugifyID(in); got != want {
			t.Errorf("SlugifyID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLegacyImportSearch(t *testing.T) {
	ctx := context.Background()
	reg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{userenv.ProxyKind(), search.Kind()},
		append(userenv.ProxySlots(), search.Slots()...),
	)
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	if err := cat.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	in := settingitems.LegacyInput{
		RawProxyURLs: []string{"http://10.0.0.8:8080"},
		Search: &settingitems.LegacySearch{
			Endpoint: "https://search.internal.example",
			APIKey:   "tvly-legacy",
			ProxyURL: "http://10.0.0.8:8080",
		},
	}
	if err := settingitems.LegacyImport(ctx, cat, in); err != nil {
		t.Fatalf("import: %v", err)
	}

	// The single-value proxy URL became an item and the search item points at it.
	proxy, ok := cat.Snapshot().Item(settingitems.KindProxy, "egress-10.0.0.8-8080")
	if !ok || proxy.Secret("url") != "http://10.0.0.8:8080" {
		t.Fatalf("synthesized proxy: %+v", proxy)
	}
	cfg := search.Resolve(cat.Snapshot(), settingitems.SlotSearchDefault, "")
	if cfg.Endpoint != "https://search.internal.example" || cfg.APIKey != "tvly-legacy" {
		t.Fatalf("resolved search config: %+v", cfg)
	}
	if cfg.ProxyURL != "http://10.0.0.8:8080" {
		t.Fatalf("search egress proxy: %+v", cfg)
	}

	// Re-running adds nothing.
	if err := settingitems.LegacyImport(ctx, cat, in); err != nil {
		t.Fatal(err)
	}
	if got := len(cat.Snapshot().Items(settingitems.KindProxy, true)); got != 1 {
		t.Fatalf("expected one proxy item after re-import, got %d", got)
	}
	if got := len(cat.Snapshot().Items(settingitems.KindSearch, true)); got != 1 {
		t.Fatalf("expected one search item after re-import, got %d", got)
	}
}

func TestResolveUnconfiguredSearchIsEmpty(t *testing.T) {
	reg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{search.Kind()},
		search.Slots(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	if err := cat.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cfg := search.Resolve(cat.Snapshot(), settingitems.SlotSearchDefault, ""); cfg.Endpoint != "" || cfg.APIKey != "" {
		t.Fatalf("unresolved slot must yield an empty config: %+v", cfg)
	}
}

func TestLegacyImportBrowser(t *testing.T) {
	ctx := context.Background()
	reg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{browser.Kind()},
		browser.Slots(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	if err := cat.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	// A default install (auto, no endpoint) stays item-free.
	if err := settingitems.LegacyImport(ctx, cat, settingitems.LegacyInput{
		Browser: &settingitems.LegacyBrowser{Provider: "auto"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(cat.Snapshot().Items(settingitems.KindBrowser, true)); got != 0 {
		t.Fatalf("default config must not create items, got %d", got)
	}
	fallback := browser.Profile{Provider: "auto", Port: 3000}
	if got := browser.Resolve(cat.Snapshot(), settingitems.SlotBrowserDefault, "", fallback); got.Provider != "auto" {
		t.Fatalf("unresolved slot must fall back to config: %+v", got)
	}

	// A configured remote source becomes an item and the global default.
	if err := settingitems.LegacyImport(ctx, cat, settingitems.LegacyInput{
		Browser: &settingitems.LegacyBrowser{
			Provider: "remote", Endpoint: "wss://cdp.example.com", Token: "tok",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := cat.Snapshot().Item(settingitems.KindBrowser, "remote"); !ok {
		t.Fatal("remote browser item missing")
	}
	got := browser.Resolve(cat.Snapshot(), settingitems.SlotBrowserDefault, "", fallback)
	if got.Provider != "remote" || got.Endpoint != "wss://cdp.example.com" || got.Token != "tok" {
		t.Fatalf("resolved profile: %+v", got)
	}
}

func TestLegacyImportLLMBindsEveryConsumer(t *testing.T) {
	ctx := context.Background()
	reg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{llmgw.Kind()},
		llmgw.Slots(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	if err := cat.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if err := settingitems.LegacyImport(ctx, cat, settingitems.LegacyInput{
		LLM: &settingitems.LegacyLLM{
			OpenAI:         settingitems.LegacyUpstream{BaseURL: "https://api.openai.com/v1", APIKey: "sk-o"},
			Anthropic:      settingitems.LegacyUpstream{BaseURL: "https://api.anthropic.com", APIKey: "sk-a"},
			DefaultModel:   "nvidia/nemotron",
			EmbeddingAlias: llmgw.EmbeddingModelAlias,
			EmbeddingModel: "text-embedding-3-small",
		},
	}); err != nil {
		t.Fatalf("import: %v", err)
	}

	// The sandbox agent keeps the Anthropic endpoint, like the pre-item env.
	agent, ok := llmgw.Resolve(cat.Snapshot(), settingitems.SlotLLMAgent, "")
	if !ok || agent.ItemID != "anthropic" || agent.Model != "nvidia/nemotron" {
		t.Fatalf("agent target: %+v ok=%v", agent, ok)
	}
	classifier, ok := llmgw.Resolve(cat.Snapshot(), settingitems.SlotLLMClassifier, "")
	if !ok || classifier.ItemID != "openai" {
		t.Fatalf("classifier target: %+v ok=%v", classifier, ok)
	}
	embed, ok := llmgw.Resolve(cat.Snapshot(), settingitems.SlotLLMEmbedding, "")
	if !ok || embed.ItemID != "openai" || embed.Model != "text-embedding-3-small" {
		t.Fatalf("embedding target: %+v ok=%v", embed, ok)
	}

	// Providers project onto relay upstreams with their protocol and secrets.
	ups := llmgw.UpstreamsFromSnapshot(cat.Snapshot())
	if len(ups) != 2 {
		t.Fatalf("upstreams: %+v", ups)
	}
	byID := map[string]llmgw.Upstream{}
	for _, up := range ups {
		byID[up.Provider] = up
	}
	if byID["anthropic"].Protocol != llmgw.ProtocolAnthropic || byID["anthropic"].APIKey != "sk-a" {
		t.Fatalf("anthropic upstream: %+v", byID["anthropic"])
	}
	if byID["openai"].ModelMap[llmgw.EmbeddingModelAlias] != "text-embedding-3-small" {
		t.Fatalf("embedding alias: %+v", byID["openai"].ModelMap)
	}

	// Re-running the import changes nothing.
	before := len(cat.Snapshot().Items(settingitems.KindLLM, true))
	if err := settingitems.LegacyImport(ctx, cat, settingitems.LegacyInput{
		LLM: &settingitems.LegacyLLM{
			OpenAI: settingitems.LegacyUpstream{BaseURL: "https://other.example", APIKey: "sk-x"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if after := len(cat.Snapshot().Items(settingitems.KindLLM, true)); after != before {
		t.Fatalf("import is not idempotent: %d -> %d", before, after)
	}
}
