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
	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/agentapi"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/api/envapi"
	"github.com/RoundpenAI/roundpen/internal/api/httpapi"
	"github.com/RoundpenAI/roundpen/internal/api/platform"
	"github.com/RoundpenAI/roundpen/internal/api/workspaceapi"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/browsetask"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/hostsetup"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/memory"
	"github.com/RoundpenAI/roundpen/internal/oauth"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// newCoreMux creates the API mux and mounts the auth, platform, native HTTP,
// browser and preview handlers.
func newCoreMux(cfg *config.Config, mgr sandbox.Manager, tplSvc *template.Service, browserHub *browser.Hub, userStore storage.UserStore, sessionStore storage.SessionStore, allowRegistration func() bool) *http.ServeMux {
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
	}
	previewHandler.Mount(mux)
	return mux
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
	envHandler.Mount(mux)
	(&workspaceapi.Handler{
		Envs:  envSvc,
		Files: sbSvc,
	}).Mount(mux)
	(&gitcred.Handler{Store: gitStore}).Mount(mux)
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
func mountLLMGateway(ctx context.Context, mux *http.ServeMux, db *storage.DB, cfg *config.Config, secretBox *secretbox.Box, memStore *memory.PgStore, memSvc *memory.Service, logger *slog.Logger) (*llmgw.Gateway, func(context.Context) error, func(context.Context, string) string) {
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

	reconfigureLLMGW := func(ctx context.Context) error {
		if err := gw.ApplyConfig(ctx, cfg.LLMGW); err != nil {
			return err
		}
		if cfg.LLMGW.Enabled && cfg.LLMGW.OpenAI != nil {
			memSvc.Embed = gw
		} else {
			memSvc.Embed = nil
		}
		logger.Info("llmgw reconfigured",
			slog.Bool("enabled", cfg.LLMGW.Enabled),
			slog.Bool("openai", cfg.LLMGW.OpenAI != nil),
			slog.Bool("anthropic", cfg.LLMGW.Anthropic != nil),
			slog.Int("virtual_keys", len(cfg.LLMGW.VirtualKeys)),
			slog.String("embedding_model", cfg.LLMGW.EmbeddingModel),
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
func newSettingsService(settingsStore *settings.Store, cfg *config.Config, appSettings settings.AppSettings, setAllowRegistration func(bool), sbSvc *sandbox.Service, tplSvc *template.Service, probe *runtime.Probe, reconfigureLLMGW func(context.Context) error) *settings.Service {
	settingsSvc := settings.NewService(settingsStore, cfg, settings.RuntimeDeps{
		AllowPublicReg:   setAllowRegistration,
		Sandbox:          sbSvc,
		Templates:        tplSvc,
		Probe:            probe,
		ReconfigureLLMGW: reconfigureLLMGW,
		LlmgwMounted:     true,
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
func newLLMPlanner(cfg *config.Config, publicURL string, gw *llmgw.Gateway) func(context.Context, hostsetup.WizardContext, hostsetup.HostFacts) (hostsetup.Plan, error) {
	return func(ctx context.Context, w hostsetup.WizardContext, f hostsetup.HostFacts) (hostsetup.Plan, error) {
		if !cfg.LLMGW.Enabled || cfg.LLMGW.OpenAI == nil {
			return hostsetup.Plan{}, fmt.Errorf("openai upstream not configured")
		}
		p := &hostsetup.LLMPlanner{
			BaseURL: publicURL,
			APIKey:  gw.InternalKey(),
			Model:   cfg.LLMGW.DefaultModel,
		}
		return p.Plan(ctx, w, f)
	}
}

// wireEnvService applies the late gateway, user and proxy wiring to the
// environment service and returns the settings-driven proxy resolver.
func wireEnvService(envSvc *userenv.Service, envHandler *envapi.Handler, settingsSvc *settings.Service, userStore storage.UserStore, publicURL string, gw *llmgw.Gateway, userVKey func(context.Context, string) string) func(string) string {
	envSvc.SetGateway(publicURL, gw.InternalKey())
	envSvc.Config.UserVirtualKey = userVKey
	envSvc.Config.DefaultModel = gw.DefaultModel
	// Egress proxy resolution needs settingsSvc, which is built after the
	// environment service; wire both selectors here.
	envHandler.Proxies = func() []settings.ProxyProfile { return settingsSvc.Current().Proxies }
	resolveProxy := func(id string) string {
		p, ok := settingsSvc.Current().ProxyByID(id)
		if !ok {
			return ""
		}
		return p.URL
	}
	envSvc.Proxy = func(ctx context.Context, userID, slot string) string {
		u, err := userStore.GetByUsername(ctx, userID)
		if err != nil || u == nil {
			return ""
		}
		if slot == userenv.SlotBrowser {
			return resolveProxy(u.BrowserProxy)
		}
		return resolveProxy(u.AgentProxy)
	}
	return resolveProxy
}

// newAutoEvaluator builds the LLM-backed auto-mode classifier evaluator.
func newAutoEvaluator(loopback string, settingsSvc *settings.Service, gw *llmgw.Gateway) *automode.LLMEvaluator {
	autoEvaluator := &automode.LLMEvaluator{
		BaseURL: strings.TrimRight(loopback, "/") + "/llmgw/openai",
		APIKey:  gw.InternalKey(),
		Model: func() string {
			if m := settingsSvc.Current().AutoMode.ClassifierModel(); m != "" {
				return m
			}
			return gw.DefaultModel()
		},
		Rules: func() automode.Rules { return settingsSvc.Current().AutoMode.Rules() },
	}
	return autoEvaluator
}

// newACPManager builds the ACP session manager with its system dependencies.
func newACPManager(loopback string, mgr sandbox.Manager, envSvc *userenv.Service, browserHub *browser.Hub, agentStore *agentsession.Store, settingsSvc *settings.Service, gw *llmgw.Gateway, autoEvaluator *automode.LLMEvaluator, logger *slog.Logger) *manager.Manager {
	acpMgr := manager.New(logger, mgr, providers.Default(), manager.SysDeps{
		LoopbackBase: loopback,
		LLMKey:       gw.InternalKey(),
		DefaultModel: gw.DefaultModel,
		AutoMode:     autoEvaluator,
		BrowserHub:   browserHub,
		BrowserSlots: envSvc,
		AgentSlots:   envSvc,
		History:      agentStore,

		WebSearch: func() (string, string, string) {
			s := settingsSvc.Current()
			return s.WebSearchEndpoint, s.WebSearchApiKey, s.WebSearchProxy
		},
	})
	return acpMgr
}

// newAgentAPI builds the agent API handler and its assist-ticket store.
func newAgentAPI(cfg *config.Config, db *storage.DB, logger *slog.Logger, agentStore *agentsession.Store, mgr sandbox.Manager, gw *llmgw.Gateway, browserHub *browser.Hub, envSvc *userenv.Service, acpMgr *manager.Manager, autoEvaluator *automode.LLMEvaluator, publicURL string, resolveProxy func(string) string, userVKey func(context.Context, string) string) (*agentapi.Handler, *assistticket.Store) {
	provisioner := &agentenv.Provisioner{
		Sandboxes: mgr,
		Config: agentenv.Config{
			PublicURL:  publicURL,
			TemplateID: cfg.DefaultAgentTemplate,
			Category:   "Agent",
		},
	}
	agentHandler := &agentapi.Handler{
		Log:         logger,
		Store:       agentStore,
		PublicURL:   firstNonEmpty(cfg.PreviewPublicURL, cfg.LLMGW.PublicURL),
		ACP:         acpMgr,
		Provisioner: provisioner,
		Sandboxes:   mgr,
		LLMGW:       gw,
		Hub:         browserHub,
		Envs:        envSvc,
		Tasks:       &browsetask.Store{DB: db.SQL},
		Tickets:     nil, // set below after ticketStore
		DestroySbx:  true,
		AutoMode:    autoEvaluator,
		ProxyURL: func(u *storage.User) string {
			if u == nil {
				return ""
			}
			return resolveProxy(u.AgentProxy)
		},
		UserVirtualKey: userVKey,
	}
	ticketStore := &assistticket.Store{DB: db.SQL}
	agentHandler.Tickets = ticketStore
	return agentHandler, ticketStore
}
