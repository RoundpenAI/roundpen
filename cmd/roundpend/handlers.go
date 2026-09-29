package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/agentapi"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/api/envapi"
	"github.com/RoundpenAI/roundpen/internal/api/httpapi"
	"github.com/RoundpenAI/roundpen/internal/api/platform"
	"github.com/RoundpenAI/roundpen/internal/api/workspaceapi"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/browsetask"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/hostaccess"
	"github.com/RoundpenAI/roundpen/internal/hostsetup"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/memory"
	"github.com/RoundpenAI/roundpen/internal/oauth"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/search"
	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// newCoreMux creates the API mux and mounts the auth, platform, native HTTP,
// browser and preview handlers. It also returns the preview handler, whose
// host router has to wrap the finished handler stack (see main).
func newCoreMux(cfg *config.Config, mgr sandbox.Manager, tplSvc *template.Service, browserHub *browser.Hub, userStore storage.UserStore, sessionStore storage.SessionStore, allowRegistration func() bool) (*http.ServeMux, *preview.Handler) {
	mux := http.NewServeMux()
	auth.Mount(mux, userStore, sessionStore, allowRegistration)
	(&platform.Handler{Manager: mgr, Templates: tplSvc}).Mount(mux)
	native := &httpapi.Handler{Manager: mgr, PublicURL: firstNonEmpty(cfg.PreviewPublicURL, cfg.LLMGW.PublicURL)}
	native.Mount(mux)
	native.MountTerminal(mux)
	(&browser.Handler{Sandboxes: mgr, Hub: browserHub}).Mount(mux)
	previewHandler := &preview.Handler{
		Manager:   mgr,
		Tokens:    preview.NewStore(cfg.PreviewTokenTTL),
		PublicURL: cfg.PreviewPublicURL,
		Domain:    cfg.PreviewDomain,
		Scheme:    cfg.PreviewDomainScheme,
	}
	previewHandler.Mount(mux)
	return mux, previewHandler
}

// mountEnvStack builds the user environment, git credential, OAuth, runtime and
// host-setup services and mounts their handlers.
func mountEnvStack(mux *http.ServeMux, db *storage.DB, cfg *config.Config, mgr sandbox.Manager, sbSvc *sandbox.Service, userStore storage.UserStore, sessionStore storage.SessionStore, probe *runtime.Probe, allowRegistration func() bool) (*userenv.Service, *envapi.Handler, *oauth.Service, *hostsetup.Service) {
	envStore := &userenv.Store{DB: db.SQL}
	gitStore := &gitcred.Store{DB: db.SQL}
	oauthStore := &oauth.Store{DB: db.SQL}
	oauthSvc := &oauth.Service{
		Store:             oauthStore,
		Users:             userStore,
		AllowRegistration: allowRegistration,
	}
	envSvc := &userenv.Service{
		Store:          envStore,
		Sandboxes:      mgr,
		Git:            gitStore,
		IdentityTokens: oauthSvc,
		Probe:          probe,
		Cfg:            cfg,
		ModelSource: func(ctx context.Context, userID string) string {
			u, err := userStore.GetByUsername(ctx, userID)
			if err != nil || u == nil {
				return ""
			}
			return u.ModelSource
		},
		Config: userenv.Config{
			BrowserTemplate: cfg.DefaultBrowserTemplate,
			AgentTemplate:   cfg.DefaultAgentTemplate,
		},
	}
	envHandler := &envapi.Handler{
		Envs:  envSvc,
		Users: userStore,
		Cfg:   cfg,
		Dial:  sbSvc,
	}
	// Credential edits must reach a sandbox that is already running: a token
	// that never expires would otherwise only land there after a rebuild. The
	// request context is detached because the browser is already redirecting.
	reinject := func(ctx context.Context, userID string) {
		envSvc.ReinjectGit(context.WithoutCancel(ctx), userID)
	}
	oauthSvc.OnIdentityChange = reinject

	envHandler.Mount(mux)
	(&workspaceapi.Handler{
		Envs:  envSvc,
		Files: sbSvc,
	}).Mount(mux)
	(&gitcred.Handler{Store: gitStore, OnChange: reinject}).Mount(mux)
	(&oauth.Handler{Svc: oauthSvc, Sessions: sessionStore}).Mount(mux)
	(&runtime.Handler{Probe: probe}).Mount(mux)

	setupSvc := &hostsetup.Service{
		Probe:  probe,
		Runner: hostsetup.NewRunner(hostsetup.RunnerConfig{RepoRoot: hostsetup.FindRepoRoot()}),
		Cfg:    cfg,
	}
	(&hostsetup.Handler{Svc: setupSvc, Cfg: cfg}).Mount(mux)
	return envSvc, envHandler, oauthSvc, setupSvc
}

// mountLLMGateway builds the LLM gateway on the mux and returns it with its
// config applier and per-user virtual key lookup.
func mountLLMGateway(ctx context.Context, mux *http.ServeMux, db *storage.DB, cfg *config.Config, secretBox *secretbox.Box, memStore *memory.PgStore, memSvc *memory.Service, items *settingitems.Catalog, logger *slog.Logger) (*llmgw.Gateway, func(context.Context) error, func(context.Context, string) string) {
	gw := llmgw.New(db, llmgw.Options{
		LogBodyMaxBytes: cfg.LLMGW.LogBodyMaxBytes,
		PublicURL:       cfg.LLMGW.PublicURL,
		Logger:          logger,
		InternalKeyFile: filepath.Join(cfg.DataRoot, "llmgw-internal.key"),
		SecretBox:       secretBox,
	})
	gw.Mount(mux)
	userKeys := llmgw.NewUserKeyManager(gw.Store(), filepath.Join(cfg.DataRoot, "vk"))
	userVKey := func(ctx context.Context, username string) string {
		k, err := userKeys.KeyFor(ctx, username)
		if err != nil {
			logger.Warn("resolve user virtual key", slog.String("user", username), slog.Any("err", err))
			return ""
		}
		return k
	}
	go memory.RunReembed(ctx, memStore, gw, logger, 2*time.Minute)

	// Providers live as setting items: project them into the relay, and the
	// embedding slot into the embedder, on boot and after every change.
	applyItems := func() {
		gw.SetUpstreams(llmgw.UpstreamsFromSnapshot(items.Snapshot()))
		target, ok := llmgw.Resolve(items.Snapshot(), settingitems.SlotLLMEmbedding, "")
		if ok {
			gw.SetEmbedding(target.ItemID, target.Model)
		} else {
			gw.SetEmbedding("", "")
		}
		if cfg.LLMGW.Enabled && ok {
			memSvc.Embed = gw
		} else {
			memSvc.Embed = nil
		}
	}
	items.OnReload(applyItems)

	reconfigureLLMGW := func(ctx context.Context) error {
		if err := gw.ApplyConfig(ctx, cfg.LLMGW); err != nil {
			return err
		}
		applyItems()
		upstreams := gw.Upstreams()
		logger.Info("llmgw reconfigured",
			slog.Bool("enabled", cfg.LLMGW.Enabled),
			slog.Int("providers", len(upstreams)),
			slog.Int("virtual_keys", len(cfg.LLMGW.VirtualKeys)),
		)
		return nil
	}
	if err := reconfigureLLMGW(ctx); err != nil {
		logger.Error("llmgw configure", slog.Any("err", err))
		os.Exit(1)
	}
	return gw, reconfigureLLMGW, userVKey
}

// newSettingsService builds the runtime settings service over the boot settings.
func newSettingsService(settingsStore *settings.Store, cfg *config.Config, appSettings settings.AppSettings, setAllowRegistration func(bool), setAgentImage func(string), sbSvc *sandbox.Service, tplSvc *template.Service, probe *runtime.Probe, reconfigureLLMGW func(context.Context) error, items *settingitems.Catalog) *settings.Service {
	// The stored value is what the first Agent sandbox must use; later PUTs
	// reach the environment service through SetAgentImage.
	if setAgentImage != nil {
		setAgentImage(appSettings.AgentImage)
	}
	settingsSvc := settings.NewService(settingsStore, cfg, settings.RuntimeDeps{
		AllowPublicReg:   setAllowRegistration,
		Sandbox:          sbSvc,
		Templates:        tplSvc,
		Probe:            probe,
		ReconfigureLLMGW: reconfigureLLMGW,
		LlmgwMounted:     true,
		SetAgentImage:    setAgentImage,
		BrowserProfile: func(itemID string) (browser.Profile, bool) {
			if itemID == "" {
				return browser.Resolve(items.Snapshot(), settingitems.SlotBrowserDefault, "",
					browser.DefaultProfile(cfg)), true
			}
			it, ok := items.Snapshot().Item(settingitems.KindBrowser, itemID)
			if !ok {
				return browser.Profile{}, false
			}
			return browser.ProfileFromItem(it), true
		},
	}, appSettings)
	return settingsSvc
}

// publicBaseURL picks the externally reachable base URL used for sandbox
// callbacks and host setup.
func publicBaseURL(cfg *config.Config) string {
	publicURL := cfg.LLMGW.PublicURL
	if publicURL == "" {
		publicURL = cfg.PreviewPublicURL
	}
	if publicURL == "" {
		// HTTPAddr is a listen address (":19001" or "0.0.0.0:19001"), not a URL.
		// Concatenating it onto http://127.0.0.1 produced http://127.0.0.10.0.0.0:19001.
		publicURL = sysagent.LoopbackBase(cfg.HTTPAddr)
	}
	return publicURL
}

// newLLMPlanner returns the host-setup wizard's LLM planning hook.
func newLLMPlanner(cfg *config.Config, publicURL string, gw *llmgw.Gateway, items *settingitems.Catalog) func(context.Context, hostsetup.WizardContext, hostsetup.HostFacts) (hostsetup.Plan, error) {
	return func(ctx context.Context, w hostsetup.WizardContext, f hostsetup.HostFacts) (hostsetup.Plan, error) {
		if !cfg.LLMGW.Enabled {
			return hostsetup.Plan{}, fmt.Errorf("llm gateway is disabled")
		}
		target, ok := llmgw.Resolve(items.Snapshot(), settingitems.SlotLLMPlanner, "")
		if !ok {
			return hostsetup.Plan{}, fmt.Errorf("no provider selected for the setup planner")
		}
		p := &hostsetup.LLMPlanner{
			BaseURL: publicURL + target.RelayPath(),
			APIKey:  gw.InternalKey(),
			Model:   target.Model,
		}
		return p.Plan(ctx, w, f)
	}
}

// wireEnvService applies the late gateway and proxy wiring to the environment
// service and returns the per-user egress proxy resolver used when starting
// agent sessions.
func wireEnvService(envSvc *userenv.Service, publicURL string, gw *llmgw.Gateway, userVKey func(context.Context, string) string, items *settingitems.Catalog) func(*storage.User) string {
	envSvc.SetGateway(publicURL, gw.InternalKey())
	envSvc.Config.UserVirtualKey = userVKey
	envSvc.Config.LLMEnv = llmEnvFor(items, publicURL)
	// Egress proxies are catalog items: resolve the user's selection per call
	// so admin and personal changes apply without a restart.
	envSvc.Proxy = func(_ context.Context, userID, slot string) string {
		return userenv.ProxyURL(items.Snapshot(), userenv.ProxySlotKey(slot), userID)
	}
	envSvc.BrowserProfile = func(userID string) browser.Profile {
		return browser.Resolve(items.Snapshot(), settingitems.SlotBrowserDefault, userID, browser.DefaultProfile(envSvc.Cfg))
	}
	return func(u *storage.User) string {
		if u == nil {
			return ""
		}
		return userenv.ProxyURL(items.Snapshot(), settingitems.SlotProxyAgent, u.Username)
	}
}

// envRebuilder recreates the sandbox a slot feeds after the user changes their
// selection: sandbox environment variables are fixed at creation time.
type envRebuilder struct{ svc *userenv.Service }

func (e envRebuilder) RebuildForSlot(ctx context.Context, username, slot string) (string, any, error) {
	var res *userenv.UpgradeResult
	var err error
	if slot == settingitems.SlotProxyBrowser {
		res, err = e.svc.RecreateBrowser(ctx, username)
	} else {
		res, err = e.svc.RecreateAgent(ctx, username)
	}
	if err != nil {
		return "", nil, err
	}
	return res.Status, res.Environment, nil
}

// importLegacyItems seeds items and bindings from pre-item configuration: the
// document an older release wrote plus the environment. It is a no-op once a
// kind already has items, and per-user proxy selections arrive through the
// schema migration that copies them out of the users table.
func importLegacyItems(ctx context.Context, cat *settingitems.Catalog, legacy settings.LegacyDocument, logger *slog.Logger) error {
	if len(legacy.Proxies) == 0 && legacy.WebSearchEndpoint == "" && legacy.WebSearchApiKey == "" &&
		legacy.WebSearchProxy == "" && legacy.CDPEndpoint == "" && legacy.CDPToken == "" &&
		legacy.LlmgwOpenaiAPIKey == "" && legacy.LlmgwAnthropicAPIKey == "" &&
		(legacy.CDPProvider == "" || legacy.CDPProvider == "auto") {
		return nil
	}
	rawURLs := []string{legacy.WebSearchProxy, legacy.LlmgwOpenaiProxy, legacy.LlmgwAnthropicProxy}
	proxies := make([]settingitems.LegacyProxy, 0, len(legacy.Proxies))
	for _, p := range legacy.Proxies {
		proxies = append(proxies, settingitems.LegacyProxy{
			ID: p.ID, Name: p.Name, URL: p.URL, Description: p.Description,
		})
	}
	in := settingitems.LegacyInput{
		Proxies:      proxies,
		RawProxyURLs: rawURLs,
	}
	if legacy.WebSearchEndpoint != "" || legacy.WebSearchApiKey != "" || legacy.WebSearchProxy != "" {
		in.Search = &settingitems.LegacySearch{
			Endpoint: legacy.WebSearchEndpoint,
			APIKey:   legacy.WebSearchApiKey,
			ProxyURL: legacy.WebSearchProxy,
		}
	}
	in.LLM = &settingitems.LegacyLLM{
		OpenAI: settingitems.LegacyUpstream{
			BaseURL:  legacy.LlmgwOpenaiBaseURL,
			APIKey:   legacy.LlmgwOpenaiAPIKey,
			ProxyURL: legacy.LlmgwOpenaiProxy,
		},
		Anthropic: settingitems.LegacyUpstream{
			BaseURL:  legacy.LlmgwAnthropicBaseURL,
			APIKey:   legacy.LlmgwAnthropicAPIKey,
			ProxyURL: legacy.LlmgwAnthropicProxy,
		},
		DefaultModel:   legacy.LlmgwDefaultModel,
		EmbeddingAlias: llmgw.EmbeddingModelAlias,
		EmbeddingModel: legacy.LlmgwEmbeddingModel,
	}
	if legacy.CDPEndpoint != "" || legacy.CDPToken != "" || (legacy.CDPProvider != "" && legacy.CDPProvider != "auto") {
		in.Browser = &settingitems.LegacyBrowser{
			Provider: legacy.CDPProvider,
			Endpoint: legacy.CDPEndpoint,
			Token:    legacy.CDPToken,
			Port:     legacy.CDPPort,
		}
	}
	if err := settingitems.LegacyImport(ctx, cat, in); err != nil {
		return err
	}
	logger.Info("setting items imported", slog.Int("proxies", len(proxies)))
	return nil
}

// newAutoEvaluator builds the LLM-backed auto-mode classifier evaluator. The
// endpoint is resolved per evaluation so binding the classifier to another
// provider applies without a restart.
func newAutoEvaluator(loopback string, settingsSvc *settings.Service, gw *llmgw.Gateway, items *settingitems.Catalog) *automode.LLMEvaluator {
	base := strings.TrimRight(loopback, "/")
	autoEvaluator := &automode.LLMEvaluator{
		Endpoint: func() (string, string) {
			target, ok := llmgw.Resolve(items.Snapshot(), settingitems.SlotLLMClassifier, "")
			if !ok {
				return "", ""
			}
			return base + target.RelayPath(), target.Model
		},
		APIKey: gw.InternalKey(),
		Rules:  func() automode.Rules { return settingsSvc.Current().AutoMode.Rules() },
	}
	return autoEvaluator
}

// llmModeSlots are the per-mode bindings that pin a model and export provider
// descriptors under ROUNDPEN_LLM_<MODE>_*.
var llmModeSlots = []struct {
	slot string
	name string
}{
	{settingitems.SlotLLMPlan, "PLAN"},
	{settingitems.SlotLLMVision, "VISION"},
	{settingitems.SlotLLMCoding, "CODING"},
}

// llmEnvFor composes the relay env a user's agent sandbox receives: the agent
// slot's provider for its protocol plus the first provider of the other
// protocol, so Claude Code and OpenAI-compatible CLIs both have an endpoint.
// Mode slots pin client variables only when they use the agent's provider —
// ANTHROPIC_BASE_URL is single-valued, so another provider cannot route there.
func llmEnvFor(items *settingitems.Catalog, loopback string) func(userID string) agentenv.LLMEnv {
	return func(userID string) agentenv.LLMEnv {
		snap := items.Snapshot()
		target, ok := llmgw.Resolve(snap, settingitems.SlotLLMAgent, userID)
		if !ok {
			return agentenv.LLMEnv{}
		}
		env := agentenv.LLMEnv{
			AnthropicProvider: strings.TrimPrefix(targetRelayPath(snap, target, llmgw.ProtocolAnthropic), "/llmgw/"),
			OpenAIProvider:    strings.TrimPrefix(targetRelayPath(snap, target, llmgw.ProtocolOpenAI), "/llmgw/"),
			Model:             target.Model,
			ModeModels:        map[string]string{},
			Extra: map[string]string{
				"ROUNDPEN_LLM_PROVIDER": target.ItemID,
				"ROUNDPEN_LLM_PROTOCOL": target.Protocol,
				"ROUNDPEN_LLM_BASE_URL": loopback + target.RelayPath(),
				"ROUNDPEN_LLM_MODEL":    target.Model,
			},
		}
		for _, mode := range llmModeSlots {
			resolved, ok := llmgw.Resolve(snap, mode.slot, userID)
			if !ok {
				continue
			}
			if resolved.ItemID == target.ItemID {
				// Same provider: the mode can pin its model on the client.
				if resolved.Model != "" {
					env.ModeModels[mode.slot] = resolved.Model
				}
				continue
			}
			// Another provider: ANTHROPIC_BASE_URL cannot route there, so only
			// export descriptors for consumers that read them.
			env.Extra["ROUNDPEN_LLM_"+mode.name+"_PROVIDER"] = resolved.ItemID
			env.Extra["ROUNDPEN_LLM_"+mode.name+"_MODEL"] = resolved.Model
			env.Extra["ROUNDPEN_LLM_"+mode.name+"_BASE_URL"] = loopback + resolved.RelayPath()
		}
		if len(env.ModeModels) == 0 {
			env.ModeModels = nil
		}
		return env
	}
}

// targetRelayPath returns the relay path for protocol: the bound provider when
// it speaks that protocol, otherwise the first provider that does.
func targetRelayPath(snap *settingitems.Snapshot, bound llmgw.Target, protocol string) string {
	if bound.Protocol == protocol {
		return bound.RelayPath()
	}
	if fallback, ok := llmgw.ProtocolFallback(snap, protocol); ok {
		return fallback.RelayPath()
	}
	return ""
}

// llmConfigFor resolves the System Agent's provider for a user.
func llmConfigFor(items *settingitems.Catalog, loopback, key string) func(userID string) sysagent.LLMConfig {
	base := strings.TrimRight(loopback, "/")
	return func(userID string) sysagent.LLMConfig {
		target, ok := llmgw.Resolve(items.Snapshot(), settingitems.SlotLLMSysAgent, userID)
		if !ok {
			return sysagent.LLMConfig{APIKey: key}
		}
		return sysagent.LLMConfig{
			BaseURL: base + target.RelayPath(),
			APIKey:  key,
			Model:   target.Model,
		}
	}
}

// newACPManager builds the ACP session manager with its system dependencies.
func newACPManager(loopback, previewZone string, mgr sandbox.Manager, envSvc *userenv.Service, tplSvc *template.Service, browserHub *browser.Hub, agentStore *agentsession.Store, assistantStore *assistant.Store, settingsSvc *settings.Service, gw *llmgw.Gateway, autoEvaluator *automode.LLMEvaluator, items *settingitems.Catalog, logger *slog.Logger) *manager.Manager {
	// Grant-gated host reads (NAS shares) for the agent's Read/Glob tools.
	hostRead := hostaccess.New(agentStore, assistantStore, logger)
	acpMgr := manager.New(logger, mgr, providers.Default(), manager.SysDeps{
		LoopbackBase: loopback,
		LLMKey:       gw.InternalKey(),
		LLM:          llmConfigFor(items, loopback, gw.InternalKey()),
		AutoMode:     autoEvaluator,
		BrowserHub:   browserHub,
		BrowserSlots: envSvc,
		AgentSlots:   envSvc,
		PreviewZone:  previewZone,
		History:      agentStore,

		Roundpen: &tools.RoundpenBinder{
			Envs:      envSvc,
			Templates: tplSvc,
			Sessions:  agentStore,
			Settings:  settingsSvc,
		},

		WebSearch: func(userID string) search.Config {
			return search.Resolve(items.Snapshot(), settingitems.SlotSearchDefault, userID)
		},
		HostFiles: hostRead.ForSession,
	})
	return acpMgr
}

// newAgentAPI builds the agent API handler and its assist-ticket store.
func newAgentAPI(cfg *config.Config, db *storage.DB, logger *slog.Logger, agentStore *agentsession.Store, mgr sandbox.Manager, gw *llmgw.Gateway, browserHub *browser.Hub, envSvc *userenv.Service, acpMgr *manager.Manager, autoEvaluator *automode.LLMEvaluator, publicURL string, proxyForUser func(*storage.User) string, userVKey func(context.Context, string) string, items *settingitems.Catalog) (*agentapi.Handler, *assistticket.Store, *agentenv.Provisioner) {
	provisioner := &agentenv.Provisioner{
		Sandboxes: mgr,
		Config: agentenv.Config{
			PublicURL:  publicURL,
			TemplateID: cfg.DefaultAgentTemplate,
			Category:   "Agent",
		},
	}
	agentHandler := &agentapi.Handler{
		Log:            logger,
		Store:          agentStore,
		PublicURL:      firstNonEmpty(cfg.PreviewPublicURL, cfg.LLMGW.PublicURL),
		ACP:            acpMgr,
		Provisioner:    provisioner,
		Sandboxes:      mgr,
		LLMGW:          gw,
		Hub:            browserHub,
		Envs:           envSvc,
		Tasks:          &browsetask.Store{DB: db.SQL},
		Tickets:        nil, // set below after ticketStore
		DestroySbx:     true,
		AutoMode:       autoEvaluator,
		ProxyURL:       proxyForUser,
		LLMEnv:         llmEnvFor(items, publicURL),
		UserVirtualKey: userVKey,
	}
	ticketStore := &assistticket.Store{DB: db.SQL}
	agentHandler.Tickets = ticketStore
	return agentHandler, ticketStore, provisioner
}
