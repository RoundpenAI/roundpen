// Package main is the Roundpen control-plane daemon (roundpend).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/imconnect"
	"github.com/RoundpenAI/roundpen/internal/issue"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/memory"
	"github.com/RoundpenAI/roundpen/internal/oauth"
	"github.com/RoundpenAI/roundpen/internal/policy"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/search"
	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/ui"
	"github.com/RoundpenAI/roundpen/internal/userenv"
	"github.com/RoundpenAI/roundpen/internal/workspace"
	"github.com/RoundpenAI/roundpen/internal/workspace/local"
	"github.com/RoundpenAI/roundpen/internal/workspace/sshfs"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	logger, dataRoot := newLogger(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db := openStorage(ctx, cfg, logger)
	defer db.Close()

	trust, err := httpx.ParseTrust(cfg.TrustedProxies)
	if err != nil {
		logger.Error("trusted proxies", slog.Any("err", err))
		os.Exit(1)
	}
	httpx.SetDefaultTrust(trust)

	// Master key sealing DB secrets (upstream API keys, settings payload).
	// Lives under the local DataRoot — EffectiveDataRoot may point at a
	// remote Docker host in SSH mode, but this file is read by roundpend.
	secretBox, err := secretbox.LoadOrGenerate(filepath.Join(cfg.DataRoot, "secret.key"), cfg.SecretKey, logger)
	if err != nil {
		logger.Error("secret master key", slog.Any("err", err))
		os.Exit(1)
	}

	settingsStore := settings.NewStore(db.SQL, secretBox)
	appSettings, err := settings.Bootstrap(ctx, settingsStore, cfg)
	if err != nil {
		logger.Error("settings bootstrap", slog.Any("err", err))
		os.Exit(1)
	}
	allowRegistration, setAllowRegistration := newRegistrationGate(cfg)

	userStore := storage.NewUserStore(db)
	sessionStore := storage.NewSessionStore(db)
	bootstrapAdmin(ctx, cfg, userStore, logger)

	itemsReg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{userenv.ProxyKind(), search.Kind(), browser.Kind(), llmgw.Kind()},
		append(append(append(userenv.ProxySlots(), search.Slots()...), browser.Slots()...), llmgw.Slots()...),
	)
	if err != nil {
		logger.Error("setting items registry", slog.Any("err", err))
		os.Exit(1)
	}
	itemsCat := settingitems.NewCatalog(settingitems.NewPGStore(db.SQL, secretBox), itemsReg)
	if err := itemsCat.Reload(ctx); err != nil {
		logger.Error("setting items load", slog.Any("err", err))
		os.Exit(1)
	}
	// Configuration that used to live in the settings document now seeds items:
	// the stored document wins where it has a value, the environment fills the
	// rest (fresh installs have none of those keys).
	storedLegacy, err := settingsStore.LoadLegacy(ctx)
	if err != nil {
		logger.Error("setting items legacy read", slog.Any("err", err))
		os.Exit(1)
	}
	if err := importLegacyItems(ctx, itemsCat, storedLegacy.Overlay(settings.FromConfigLegacy(cfg)), logger); err != nil {
		logger.Error("setting items legacy import", slog.Any("err", err))
		os.Exit(1)
	}
	// Items may also be written by another daemon sharing this database.
	go itemsCat.RefreshLoop(ctx, 30*time.Second)

	wsFS, err := newWorkspaceFS(cfg, dataRoot, logger)
	if err != nil {
		logger.Error("workspace", slog.Any("err", err))
		os.Exit(1)
	}

	var mgr sandbox.Manager
	var sbSvc *sandbox.Service
	store := storage.NewSandboxStore(db)
	tplStore := template.NewStore(db.SQL)
	tplSvc := template.NewService(tplStore, cfg.DefaultImage)
	if err := tplSvc.Seed(ctx, cfg.Backend); err != nil {
		logger.Error("template seed", slog.Any("err", err))
		os.Exit(1)
	}

	browserHub := browser.NewHub(dataRoot, logger)
	browserHub.SetConfig(cfg)
	defer browserHub.Close()

	bs := newBackendSet(cfg, dataRoot, wsFS, browserHub, tplSvc, store, logger)
	defer func() { _ = bs.engine.Close() }()
	mgr = bs.sandboxes
	sbSvc = bs.sandboxes
	probe := bs.probe

	mux := newCoreMux(cfg, mgr, tplSvc, browserHub, userStore, sessionStore, allowRegistration)

	envSvc, envHandler, oauthSvc, setupSvc := mountEnvStack(mux, db, cfg, mgr, sbSvc, userStore, sessionStore, probe, allowRegistration)
	(&settingitems.Handler{Cat: itemsCat, Envs: envRebuilder{svc: envSvc}}).Mount(mux)

	memStore := memory.NewPgStore(db)
	memSvc := &memory.Service{Store: memStore, Logger: logger}
	go memory.RunPurge(ctx, memStore, logger, time.Hour)

	gw, reconfigureLLMGW, userVKey := mountLLMGateway(ctx, mux, db, cfg, secretBox, memStore, memSvc, itemsCat, logger)

	settingsSvc := newSettingsService(settingsStore, cfg, appSettings, setAllowRegistration, sbSvc, tplSvc, probe, reconfigureLLMGW, itemsCat)
	(&settings.Handler{Svc: settingsSvc}).Mount(mux)

	(&memory.Handler{Store: memStore, Service: memSvc}).Mount(mux)

	publicURL := publicBaseURL(cfg)

	setupSvc.PlanLLM = newLLMPlanner(cfg, publicURL, gw, itemsCat)

	proxyForUser := wireEnvService(envSvc, publicURL, gw, userVKey, itemsCat)
	// The hub attaches to whatever the control plane resolved for a key, so
	// record the profile whenever a browser environment is resolved.
	envHandler.BrowserProfiles = func(userID, key string) {
		if key != "" {
			browserHub.SetProfile(key, envSvc.BrowserProfileFor(userID))
		}
	}
	// Safety net: when a hub key has no recorded profile (e.g. the agent tool
	// path calls Hub.Ensure before SetProfile), resolve from the settings
	// catalog so a user-configured remote/cloud endpoint is never silently
	// overridden by the docker default.
	browserHub.SetProfileResolver(func(key string) (browser.Profile, bool) {
		if uid, ok := strings.CutPrefix(key, "browser-"); ok && uid != "" {
			return envSvc.BrowserProfileFor(uid), true
		}
		return browser.Profile{}, false
	})

	agentStore := &agentsession.Store{DB: db.SQL}
	loopback := sysagent.LoopbackBase(cfg.HTTPAddr)
	if cfg := search.Resolve(itemsCat.Snapshot(), settingitems.SlotSearchDefault, ""); cfg.Endpoint != "" || cfg.APIKey != "" {
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "(default)"
		}
		logger.Info("web search enabled", "endpoint", endpoint, "api_key_set", cfg.APIKey != "")
	}
	autoEvaluator := newAutoEvaluator(loopback, settingsSvc, gw, itemsCat)

	acpMgr := newACPManager(loopback, mgr, envSvc, tplSvc, browserHub, agentStore, settingsSvc, gw, autoEvaluator, itemsCat, logger)

	agentHandler, ticketStore, provisioner := newAgentAPI(cfg, db, logger, agentStore, mgr, gw, browserHub, envSvc, acpMgr, autoEvaluator, publicURL, proxyForUser, userVKey, itemsCat)

	agentHandler.Mount(mux)
	issueStore := &issue.Store{DB: db.SQL}
	(&issue.Handler{Store: issueStore}).Mount(mux)
	assistantStore := &assistant.Store{DB: db.SQL}
	denialStore := &policy.DenialStore{DB: db.SQL}

	lang, imDataDir, imProvider := imconnect.EnvDefaults()
	var imSup *imconnect.Supervisor
	if cfg.IMEnabled {
		imSup, err = imconnect.NewSupervisor(imconnect.Deps{
			Assistants:  assistantStore,
			Store:       agentStore,
			ACP:         acpMgr,
			Users:       userStore,
			Provisioner: provisioner,
			LLMGW:       gw,
			LLMEnv:      llmEnvFor(itemsCat, publicURL),
			Provider:    imProvider,
			Lang:        lang,
			DataDir:     imDataDir,
		})
		if err != nil {
			logger.Error("imconnect supervisor", slog.Any("err", err))
			imSup = nil
		}
	} else {
		// Secondary instances sharing a DB must not race the primary one for the
		// same chat platform connections.
		logger.Info("ROUNDPEN_IM_ENABLED is off — IM engines skipped (set it to true to start them)")
	}

	asstHandler := &assistant.Handler{
		Store:    assistantStore,
		Sessions: agentStore,
		Starter:  agentHandler,
		Tickets:  ticketStore,
		Denials:  denialStore,
		IM:       imSup,
	}
	asstHandler.Mount(mux)

	if imSup != nil {
		if err := imSup.StartAll(ctx); err != nil {
			logger.Error("imconnect start", slog.Any("err", err))
		}
	}

	// Console SPA last — catch-all for non-API GET paths (embedded via internal/ui).
	mux.Handle("/", ui.Handler())

	// Gitea access tokens expire (1h by default): keep them fresh and push the
	// new token into any running agent sandbox, or long sessions lose push.
	go oauthRefresher(ctx, oauthSvc, envSvc, logger)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           auth.Middleware(userStore, sessionStore)(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // LLM relay and terminal WS stream past a write deadline
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		logger.Info("http listening", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", slog.Any("err", err))
			stop()
		}
	}()

	<-ctx.Done()
	if imSup != nil {
		imSup.StopAll()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	logger.Info("roundpend shut down")
}

func newWorkspaceFS(cfg *config.Config, dataRoot string, logger *slog.Logger) (workspace.FS, error) {
	if cfg.WorkspaceUsesSSH() {
		fs, err := sshfs.NewFromDockerHost(cfg.DockerHost, dataRoot)
		if err != nil {
			return nil, err
		}
		logger.Info("using ssh workspace fs",
			slog.String("docker_host", cfg.DockerHost),
			slog.String("remote_root", dataRoot),
		)
		return fs, nil
	}
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return nil, err
	}
	return local.New(dataRoot), nil
}

// internalDialer adapts sandbox.Service for the browser hub: the hub dials
// sandboxes from inside the control plane, so it carries an internal admin
// actor (sandbox.Service.Dial authorizes via authz).
type internalDialer struct {
	sandbox *sandbox.Service
}

func (d internalDialer) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	ctx = authz.WithActor(ctx, authz.Actor{Username: "roundpend", Admin: true})
	return d.sandbox.Dial(ctx, sandboxID, destPort)
}

// oauthRefresher refreshes expiring federated tokens and re-injects the git
// credentials of the affected users' running agent sandboxes.
func oauthRefresher(ctx context.Context, svc *oauth.Service, envs *userenv.Service, logger *slog.Logger) {
	const interval = 10 * time.Minute
	// Refresh anything expiring within 30 minutes; injected tokens must stay
	// usable for a sandbox that runs long between EnsureAgent calls.
	const horizon = 30 * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			users, err := svc.RefreshExpiring(ctx, time.Now().Add(horizon))
			if err != nil {
				logger.Warn("oauth refresh", slog.Any("err", err))
				continue
			}
			for _, user := range users {
				envs.ReinjectGit(ctx, user)
			}
			if len(users) > 0 {
				logger.Info("oauth tokens refreshed", slog.Int("users", len(users)))
			}
		}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
