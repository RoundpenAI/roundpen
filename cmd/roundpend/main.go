// Package main is the Roundpen control-plane daemon (roundpend).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
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
	"github.com/RoundpenAI/roundpen/internal/backend/multi"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/browsetask"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/memory"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
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

	dataRoot := cfg.EffectiveDataRoot()
	if abs, err := filepath.Abs(dataRoot); err == nil {
		dataRoot = abs
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	logger.Info("roundpend starting",
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("data_root", dataRoot),
		slog.String("backend", cfg.Backend),
		slog.String("docker_host", cfg.DockerHost),
		slog.String("default_image", cfg.DefaultImage),
		slog.Bool("workspace_ssh", cfg.WorkspaceUsesSSH()),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := storage.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()

	if err := db.MigrateEmbedded(ctx); err != nil {
		logger.Error("migrate", slog.Any("err", err))
		os.Exit(1)
	}

	settingsStore := settings.NewStore(db.SQL)
	appSettings, err := settings.Bootstrap(ctx, settingsStore, cfg)
	if err != nil {
		logger.Error("settings bootstrap", slog.Any("err", err))
		os.Exit(1)
	}

	var allowRegMu sync.RWMutex
	allowPublicRegistration := cfg.AllowPublicRegistration
	allowRegistration := func() bool {
		allowRegMu.RLock()
		defer allowRegMu.RUnlock()
		return allowPublicRegistration
	}
	setAllowRegistration := func(v bool) {
		allowRegMu.Lock()
		allowPublicRegistration = v
		cfg.AllowPublicRegistration = v
		allowRegMu.Unlock()
	}

	userStore := storage.NewUserStore(db)
	sessionStore := storage.NewSessionStore(db)
	if cfg.BootstrapAdmin {
		if err := auth.BootstrapAdmin(ctx, userStore, cfg.APIKey, logger); err != nil {
			logger.Error("bootstrap admin", slog.Any("err", err))
			os.Exit(1)
		}
	} else {
		logger.Info("ROUNDPEN_BOOTSTRAP_ADMIN is false — admin bootstrap skipped")
	}

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
	tplSvc.SetLogger(logger)
	if err := tplSvc.Seed(ctx, cfg.Backend); err != nil {
		logger.Error("template seed", slog.Any("err", err))
		os.Exit(1)
	}
	var closeBuilder func()
	reattachBuilder := func() error {
		if closeBuilder != nil {
			closeBuilder()
			closeBuilder = nil
		}
		var err error
		closeBuilder, err = template.AttachBuilder(cfg, tplSvc, logger)
		return err
	}
	if err := reattachBuilder(); err != nil {
		logger.Error("template builder", slog.Any("err", err))
		os.Exit(1)
	}
	defer func() {
		if closeBuilder != nil {
			closeBuilder()
		}
	}()

	browserHub := browser.NewHub(dataRoot, logger)
	browserHub.SetConfig(cfg)
	defer browserHub.Close()

	eng := multi.New(multi.Options{
		DataRoot:      dataRoot,
		DockerHost:    cfg.DockerHost,
		DockerRuntime: cfg.DockerRuntime,
		DefaultAgent:  cfg.Backend,
		DisableQEMU:   !cfg.QEMUEnabled,
	})
	eng.Warm()
	defer func() { _ = eng.Close() }()
	if err := eng.QEMUErr(); err != nil {
		logger.Warn("qemu not ready", slog.Any("err", err))
	} else {
		logger.Info("qemu backend enabled")
	}
	probe := &runtime.Probe{Cfg: cfg}
	if eng.HasDocker() {
		probe.DockerReady = true
		logger.Info("docker backend enabled")
	} else if err := eng.DockerErr(); err != nil {
		probe.DockerErr = err.Error()
		logger.Warn("docker not ready", slog.Any("err", err))
	}
	sbSvc = sandbox.NewService(store, eng, wsFS, cfg.DefaultImage, cfg.DefaultTTL, logger, sandbox.WithTemplates(tplSvc), sandbox.WithBrowser(browserHub))
	mgr = sbSvc
	logger.Info("using multi backend", slog.String("default_agent_engine", cfg.Backend))
	browserHub.SetDialer(sbSvc)

	mux := http.NewServeMux()
	auth.Mount(mux, userStore, sessionStore, allowRegistration)
	(&platform.Handler{Manager: mgr, Templates: tplSvc}).Mount(mux)
	native := &httpapi.Handler{Manager: mgr}
	native.Mount(mux)
	native.MountTerminal(mux)
	(&browser.Handler{Sandboxes: mgr, Hub: browserHub}).Mount(mux)
	previewHandler := &preview.Handler{
		Manager:   mgr,
		Tokens:    preview.NewStore(cfg.PreviewTokenTTL),
		PublicURL: cfg.PreviewPublicURL,
	}
	previewHandler.Mount(mux)

	envStore := &userenv.Store{DB: db.SQL}
	gitStore := &gitcred.Store{DB: db.SQL}
	prefStore := &runtime.PrefStore{DB: db.SQL}
	envSvc := &userenv.Service{
		Store:     envStore,
		Sandboxes: mgr,
		Git:       gitStore,
		Probe:     probe,
		Prefs:     prefStore,
		Config: userenv.Config{
			BrowserTemplate: cfg.DefaultBrowserTemplate,
			AgentTemplate:   cfg.DefaultAgentTemplate,
			DefaultEngine:   cfg.Backend,
		},
	}
	publicBase := cfg.PreviewPublicURL
	if publicBase == "" {
		publicBase = cfg.LLMGW.PublicURL
	}
	(&envapi.Handler{
		Envs:      envSvc,
		Prefs:     prefStore,
		Tokens:    previewHandler.Tokens,
		PublicURL: publicBase,
		VNC:       eng,
	}).Mount(mux)
	(&gitcred.Handler{Store: gitStore}).Mount(mux)
	(&runtime.Handler{Probe: probe, Prefs: prefStore}).Mount(mux)

	memStore := memory.NewPgStore(db)
	memSvc := &memory.Service{Store: memStore, Logger: logger}
	go memory.RunPurge(ctx, memStore, logger, time.Hour)

	gw := llmgw.New(db, llmgw.Options{
		LogBodyMaxBytes: cfg.LLMGW.LogBodyMaxBytes,
		PublicURL:       cfg.LLMGW.PublicURL,
		Logger:          logger,
	})
	gw.Mount(mux)
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

	settingsSvc := settings.NewService(settingsStore, cfg, settings.RuntimeDeps{
		AllowPublicReg:   setAllowRegistration,
		PreviewTokens:    previewHandler.Tokens,
		PreviewHandler:   previewHandler,
		Sandbox:          sbSvc,
		Templates:        tplSvc,
		ReattachBuilder:  reattachBuilder,
		ReconfigureLLMGW: reconfigureLLMGW,
		LlmgwMounted:     true,
	}, appSettings)
	(&settings.Handler{Svc: settingsSvc}).Mount(mux)

	(&memory.Handler{Store: memStore, Service: memSvc}).Mount(mux)

	publicURL := cfg.LLMGW.PublicURL
	if publicURL == "" {
		publicURL = cfg.PreviewPublicURL
	}
	if publicURL == "" {
		// HTTPAddr is a listen address (":19001" or "0.0.0.0:19001"), not a URL.
		// Concatenating it onto http://127.0.0.1 produced http://127.0.0.10.0.0.0:19001.
		publicURL = sysagent.LoopbackBase(cfg.HTTPAddr)
	}
	envSvc.SetGateway(publicURL, llmgw.InternalVirtualKey)
	envSvc.Config.DefaultModel = gw.DefaultModel
	agentStore := &agentsession.Store{DB: db.SQL}
	loopback := sysagent.LoopbackBase(cfg.HTTPAddr)
	acpMgr := manager.New(logger, mgr, providers.Default(), manager.SysDeps{
		LoopbackBase: loopback,
		LLMKey:       llmgw.InternalVirtualKey,
		DefaultModel: gw.DefaultModel,
		BrowserHub:   browserHub,
		BrowserSlots: envSvc,
		AgentSlots:   envSvc,
		History:      agentStore,
	})
	provisioner := &agentenv.Provisioner{
		Sandboxes: mgr,
		Config: agentenv.Config{
			PublicURL:  publicURL,
			TemplateID: cfg.DefaultAgentTemplate,
			Category:   "Agent",
		},
	}
	(&agentapi.Handler{
		Log:         logger,
		Store:       agentStore,
		ACP:         acpMgr,
		Provisioner: provisioner,
		Sandboxes:   mgr,
		LLMGW:       gw,
		Hub:         browserHub,
		Envs:        envSvc,
		Tasks:       &browsetask.Store{DB: db.SQL},
		DestroySbx:  true,
	}).Mount(mux)

	// Console SPA last — catch-all for non-API GET paths (embedded via internal/ui).
	mux.Handle("/", ui.Handler())

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           auth.Middleware(userStore, sessionStore)(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("http listening", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", slog.Any("err", err))
			stop()
		}
	}()

	<-ctx.Done()
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
