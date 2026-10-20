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
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/browsetask"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/connectors"
	"github.com/RoundpenAI/roundpen/internal/fnosfile"
	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/hostaccess"
	"github.com/RoundpenAI/roundpen/internal/hostsetup"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/mcp"
	"github.com/RoundpenAI/roundpen/internal/memory"
	"github.com/RoundpenAI/roundpen/internal/oauth"
	"github.com/RoundpenAI/roundpen/internal/policy"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/project"
	"github.com/RoundpenAI/roundpen/internal/routine"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/search"
	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/userenv"
	"github.com/RoundpenAI/roundpen/internal/userworkspace"
	"github.com/RoundpenAI/roundpen/internal/webpush"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// newPushService loads or generates VAPID keys and returns a push Service.
// A nil return means push is unavailable (key error); the daemon still boots.
func newPushService(cfg *config.Config, db *storage.DB, logger *slog.Logger) *webpush.Service {
	vapid, err := webpush.LoadOrGenerateVAPID(
		filepath.Join(cfg.DataRoot, "webpush-vapid.json"),
		cfg.VAPIDPublicKey,
		cfg.VAPIDPrivateKey,
		cfg.VAPIDSubject,
	)
	if err != nil {
		logger.Warn("web push vapid", slog.Any("err", err))
		return nil
	}
	return &webpush.Service{
		Store: &webpush.Store{DB: db.SQL},
		VAPID: vapid,
		Log:   logger,
	}
}

// newCoreMux creates the API mux and mounts the auth, platform, native HTTP,
// browser and preview handlers. It also returns the preview handler, whose
// host router has to wrap the finished handler stack (see main).
func newCoreMux(cfg *config.Config, mgr sandbox.Manager, tplSvc *template.Service, browserHub *browser.Hub, userStore storage.UserStore, sessionStore storage.SessionStore, allowRegistration func() bool) (*http.ServeMux, *preview.Handler, *auth.UserHandler) {
	mux := http.NewServeMux()
	userHandler := auth.Mount(mux, userStore, sessionStore, allowRegistration, cfg.BootstrapAdminPassword)
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
	return mux, previewHandler, userHandler
}

// mountEnvStack builds the user environment, git credential, OAuth, connectors,
// MCP, runtime and host-setup services and mounts their handlers.
func mountEnvStack(mux *http.ServeMux, db *storage.DB, cfg *config.Config, mgr sandbox.Manager, sbSvc *sandbox.Service, userStore storage.UserStore, sessionStore storage.SessionStore, probe *runtime.Probe, allowRegistration func() bool, items *settingitems.Catalog, secretBox *secretbox.Box) (*userenv.Service, *envapi.Handler, *oauth.Service, *connectors.Service, *mcp.Service, *hostsetup.Service, *hostsetup.Handler) {
	envStore := &userenv.Store{DB: db.SQL}
	gitStore := &gitcred.Store{DB: db.SQL}
	oauthStore := &oauth.Store{DB: db.SQL}
	oauthSvc := &oauth.Service{
		Store:             oauthStore,
		Users:             userStore,
		AllowRegistration: allowRegistration,
	}
	connectorsSvc := &connectors.Service{Store: &connectors.Store{DB: db.SQL}}
	mcpSvc := &mcp.Service{Store: &mcp.Store{DB: db.SQL, Box: secretBox}}
	envSvc := &userenv.Service{
		Store:          envStore,
		Sandboxes:      mgr,
		Git:            gitStore,
		IdentityTokens: oauthSvc,
		Users:          userStore,
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
	roots := &userworkspace.Store{DB: db.SQL}
	files := fnosfile.FromEnv()
	envSvc.WorkspaceRoot = roots.Get
	envSvc.CheckWorkspace = func(ctx context.Context, userID, root string) error {
		return checkBoundFolder(ctx, userStore, files, userID, root, true)
	}
	if sbSvc != nil {
		sbSvc.SetFileACL(func(ctx context.Context, workspacePath string, write bool) error {
			actor, ok := authz.From(ctx)
			if !ok || strings.TrimSpace(actor.Username) == "" {
				return fmt.Errorf("unauthorized")
			}
			bound, err := roots.Get(ctx, actor.Username)
			if err != nil {
				return err
			}
			if bound == "" || filepath.Clean(workspacePath) != filepath.Join(filepath.Clean(bound), "workspace") {
				return fmt.Errorf("workspace is not bound to this user")
			}
			return checkBoundFolder(ctx, userStore, files, actor.Username, bound, write)
		})
	}
	envHandler := &envapi.Handler{
		Envs:  envSvc,
		Users: userStore,
		Cfg:   cfg,
		Dial:  sbSvc,
	}
	(&userworkspace.Handler{
		Store:    roots,
		Files:    files,
		Envs:     envSvc,
		DataRoot: cfg.DataRoot,
	}).Mount(mux)
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
	(&connectors.Handler{Svc: connectorsSvc}).Mount(mux)
	(&mcp.Handler{Svc: mcpSvc}).Mount(mux)
	(&runtime.Handler{Probe: probe}).Mount(mux)

	setupSvc := &hostsetup.Service{
		Probe:  probe,
		Runner: hostsetup.NewRunner(hostsetup.RunnerConfig{RepoRoot: hostsetup.FindRepoRoot()}),
		Cfg:    cfg,
	}
	setupHandler := &hostsetup.Handler{Svc: setupSvc, Cfg: cfg, Items: items}
	setupHandler.Mount(mux)
	return envSvc, envHandler, oauthSvc, connectorsSvc, mcpSvc, setupSvc, setupHandler
}

// mountLLMGateway builds the LLM gateway on the mux and returns it with its
// config applier and per-user virtual key lookup.
func mountLLMGateway(ctx context.Context, mux *http.ServeMux, db *storage.DB, cfg *config.Config, secretBox *secretbox.Box, memStore *memory.PgStore, memSvc *memory.Service, items *settingitems.Catalog, mintInstallToken func() (string, error), installPubHex string, logger *slog.Logger) (*llmgw.Gateway, func(context.Context) error, func(context.Context, string) string) {
	gw := llmgw.New(db, llmgw.Options{
		LogBodyMaxBytes:     cfg.LLMGW.LogBodyMaxBytes,
		PublicURL:           cfg.LLMGW.PublicURL,
		Logger:              logger,
		InternalKeyFile:     filepath.Join(cfg.DataRoot, "llmgw-internal.key"),
		SecretBox:           secretBox,
		MintInstallToken:    mintInstallToken,
		InstallPublicKeyHex: installPubHex,
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
func newSettingsService(settingsStore *settings.Store, cfg *config.Config, appSettings settings.AppSettings, setAgentImage func(string), sbSvc *sandbox.Service, tplSvc *template.Service, probe *runtime.Probe, reconfigureLLMGW func(context.Context) error, items *settingitems.Catalog) *settings.Service {
	// The stored value is what the first Agent sandbox must use; later PUTs
	// reach the environment service through SetAgentImage.
	if setAgentImage != nil {
		setAgentImage(appSettings.AgentImage)
	}
	settingsSvc := settings.NewService(settingsStore, cfg, settings.RuntimeDeps{
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

// agentJobSlots adapts userenv.EnsureAgentJob to the tools.AgentSlot interface
// so routine / worker sessions do not block the interactive agent container.
type agentJobSlots struct {
	svc *userenv.Service
}

func (a agentJobSlots) EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error) {
	return a.svc.EnsureAgentJob(ctx, userID)
}

// newACPManager builds the ACP session manager with its system dependencies.
func newACPManager(loopback, previewZone string, mgr sandbox.Manager, envSvc *userenv.Service, tplSvc *template.Service, browserHub *browser.Hub, agentStore *agentsession.Store, assistantStore *assistant.Store, settingsSvc *settings.Service, gw *llmgw.Gateway, autoEvaluator *automode.LLMEvaluator, items *settingitems.Catalog, connectorsSvc *connectors.Service, mcpSvc *mcp.Service, routineStore *routine.Store, users storage.UserStore, logger *slog.Logger) *manager.Manager {
	// Grant-gated host reads (NAS shares) for the agent's Read/Glob tools.
	hostRead := hostaccess.New(agentStore, assistantStore, logger)
	var onTokens func(runKey string, delta int)
	if routineStore != nil {
		onTokens = func(runKey string, delta int) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = routineStore.AddTokensUsed(ctx, runKey, delta)
		}
	}
	acpMgr := manager.New(logger, mgr, providers.Default(), manager.SysDeps{
		LoopbackBase:    loopback,
		LLMKey:          gw.InternalKey(),
		LLM:             llmConfigFor(items, loopback, gw.InternalKey()),
		AutoMode:        autoEvaluator,
		BrowserHub:      browserHub,
		BrowserSlots:    envSvc,
		AgentSlots:      envSvc,
		AgentJobSlots:   agentJobSlots{svc: envSvc},
		OnRoutineTokens: onTokens,
		PreviewZone:     previewZone,
		History:         agentStore,
		Google:          connectorsSvc,
		MCP:             mcpSvc,
		PolicyFor: func(ctx context.Context, userID, assistantID string) *policy.Profile {
			if assistantStore == nil || assistantID == "" {
				return nil
			}
			a, err := assistantStore.Get(ctx, assistantID)
			if err != nil || a == nil || a.UserID != userID {
				return nil
			}
			return assistant.PolicyProfile(a)
		},

		Roundpen: &tools.RoundpenBinder{
			Envs:      envSvc,
			Templates: tplSvc,
			Sessions:  agentStore,
			Settings:  settingsSvc,
		},

		WebSearch: func(userID string) search.Config {
			cfg := search.Resolve(items.Snapshot(), settingitems.SlotSearchDefault, userID)
			// A dedicated search backend wins. Otherwise a built-in LLM
			// platform's own web search is used directly.
			if strings.TrimSpace(cfg.Endpoint) == "" && strings.TrimSpace(cfg.APIKey) == "" {
				if vendor := llmgw.VendorSearch(items.Snapshot(), userID); vendor.Vendor != "" {
					return vendor
				}
			}
			return cfg
		},
		HostFiles: hostRead.ForSession,
		UserContext: func(ctx context.Context, userID string) string {
			if users == nil || strings.TrimSpace(userID) == "" {
				return ""
			}
			u, err := users.GetByUsername(ctx, userID)
			if err != nil || u == nil {
				return ""
			}
			return sysagent.FormatUserContext(u.Username, u.FullName, u.OrgName, u.About, u.Timezone)
		},
	})
	return acpMgr
}

// newAgentAPI builds the agent API handler and its assist-ticket store.
func newAgentAPI(cfg *config.Config, db *storage.DB, logger *slog.Logger, agentStore *agentsession.Store, mgr sandbox.Manager, gw *llmgw.Gateway, browserHub *browser.Hub, envSvc *userenv.Service, acpMgr *manager.Manager, autoEvaluator *automode.LLMEvaluator, publicURL string, proxyForUser func(*storage.User) string, userVKey func(context.Context, string) string, items *settingitems.Catalog, push webpush.Notifier) (*agentapi.Handler, *assistticket.Store, *agentenv.Provisioner) {
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
		Push:           push,
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

// projectDirSeeder creates projects/<slug>/ and Shared Context on the host
// workspace after EnsureAgent. Failures are non-fatal at the API layer.
type projectDirSeeder struct {
	Envs interface {
		EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
	}
	Paths interface {
		WorkspaceHostPath(ctx context.Context, id string) (string, error)
	}
}

func (s projectDirSeeder) EnsureProjectTree(ctx context.Context, userID, slug string) error {
	if s.Envs == nil || s.Paths == nil {
		return fmt.Errorf("workspace seeder not configured")
	}
	sb, err := s.Envs.EnsureAgent(ctx, userID)
	if err != nil {
		return err
	}
	host, err := s.Paths.WorkspaceHostPath(ctx, sb.ID)
	if err != nil {
		return err
	}
	return workspace.SeedProject(host, slug)
}

// projectSessionProfiler restores coordinator / worker personas on reconnect.
type projectSessionProfiler struct {
	Projects interface {
		GetWorkerBySession(ctx context.Context, userID, sessionID string) (*project.Worker, error)
	}
	Assistants interface {
		Get(ctx context.Context, id string) (*assistant.Assistant, error)
	}
}

func (p projectSessionProfiler) ProfileFor(ctx context.Context, userID string, sess *agentsession.Session) string {
	if sess == nil {
		return ""
	}
	if sess.Kind == "worker" {
		if p.Projects != nil {
			if w, err := p.Projects.GetWorkerBySession(ctx, userID, sess.ID); err == nil && w != nil {
				role, _ := project.NormalizeWorkerRole(w.Role)
				return role
			}
		}
		return project.RoleImplementer
	}
	if sess.Kind == "chat" && sess.AssistantID != "" && p.Assistants != nil {
		if a, err := p.Assistants.Get(ctx, sess.AssistantID); err == nil && a != nil && a.Kind == assistant.KindCoordinator {
			return "coordinator"
		}
	}
	return ""
}

// projectWorkerStarter adapts agentapi.Handler to project.WorkerStarter.
type projectWorkerStarter struct {
	H interface {
		CreateWorkerSession(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error)
		StartWorkerRuntime(ctx context.Context, user *storage.User, sessionID, role string) error
	}
}

func (a projectWorkerStarter) CreateWorkerSession(ctx context.Context, user *storage.User, assistantID, title string) (string, error) {
	sess, err := a.H.CreateWorkerSession(ctx, user, assistantID, title)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (a projectWorkerStarter) StartWorkerRuntime(ctx context.Context, user *storage.User, sessionID, role string) error {
	return a.H.StartWorkerRuntime(ctx, user, sessionID, role)
}

// projectSessionBinder lets issue create auto-attach a project from the coordinator session.
type projectSessionBinder struct {
	Store interface {
		GetByCoordinatorSession(ctx context.Context, userID, sessionID string) (*project.Project, error)
		GetByCoordinatorAssistant(ctx context.Context, userID, assistantID string) (*project.Project, error)
	}
}

func (b projectSessionBinder) ProjectIDForSession(ctx context.Context, userID, sessionID string) (string, error) {
	if b.Store == nil {
		return "", project.ErrNotFound
	}
	p, err := b.Store.GetByCoordinatorSession(ctx, userID, sessionID)
	if err == nil {
		return p.ID, nil
	}
	return "", err
}

// issueIDResolver adapts issue.Store for project.Handler.Issues.
type issueIDResolver struct {
	Store interface {
		ResolveID(ctx context.Context, userID, keyOrID string) (string, error)
	}
}

func (r issueIDResolver) LookupID(ctx context.Context, userID, keyOrID string) (string, error) {
	return r.Store.ResolveID(ctx, userID, keyOrID)
}

func checkBoundFolder(ctx context.Context, users storage.UserStore, files *fnosfile.Client, userID, root string, write bool) error {
	if files == nil || !files.Enabled() {
		return fmt.Errorf("fnOS file API is not available")
	}
	user, err := users.GetByUsername(ctx, userID)
	if err != nil || user == nil {
		return fmt.Errorf("user not found")
	}
	uid, ok := userworkspace.FnosUID(user.AuthProvider, user.Username)
	if !ok {
		return fmt.Errorf("not an fnOS user")
	}
	acl, err := files.CheckACL(ctx, uid, root)
	if err != nil {
		return err
	}
	if write && !acl.Writable {
		return fmt.Errorf("folder is not writable")
	}
	if !write && !acl.Readable {
		return fmt.Errorf("folder is not readable")
	}
	return nil
}
