package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/backend/multi"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// newLogger builds the JSON logger and resolves the effective data root.
func newLogger(cfg *config.Config) (*slog.Logger, string) {
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
	return logger, dataRoot
}

// openStorage opens the control-plane database and applies embedded migrations.
func openStorage(ctx context.Context, cfg *config.Config, logger *slog.Logger) *storage.DB {
	db, err := storage.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres", slog.Any("err", err))
		os.Exit(1)
	}

	if err := db.MigrateEmbedded(ctx); err != nil {
		logger.Error("migrate", slog.Any("err", err))
		os.Exit(1)
	}
	return db
}

// newRegistrationGate returns the public-registration flag reader and setter
// shared by the auth handlers and the settings service.
func newRegistrationGate(cfg *config.Config) (func() bool, func(bool)) {
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
	return allowRegistration, setAllowRegistration
}

// bootstrapAdmin seeds the initial admin account when configured.
func bootstrapAdmin(ctx context.Context, cfg *config.Config, userStore storage.UserStore, logger *slog.Logger) {
	if cfg.BootstrapAdmin {
		credFile := filepath.Join(cfg.DataRoot, "bootstrap-admin-credentials.txt")
		if err := auth.BootstrapAdmin(ctx, userStore, cfg.APIKey, cfg.BootstrapAdminPassword, credFile, logger); err != nil {
			logger.Error("bootstrap admin", slog.Any("err", err))
			os.Exit(1)
		}
	} else {
		logger.Info("ROUNDPEN_BOOTSTRAP_ADMIN is false — admin bootstrap skipped")
	}
}

// backendSet bundles the container engine, host probe and sandbox service.
type backendSet struct {
	engine    *multi.Backend
	probe     *runtime.Probe
	sandboxes *sandbox.Service
}

// newBackendSet builds the sandbox engine stack and wires the browser hub to it.
func newBackendSet(cfg *config.Config, dataRoot string, wsFS workspace.FS, browserHub *browser.Hub, tplSvc *template.Service, store *storage.SandboxStore, logger *slog.Logger) *backendSet {
	eng := multi.New(multi.Options{
		DataRoot:      dataRoot,
		DockerHost:    cfg.DockerHost,
		DockerRuntime: cfg.DockerRuntime,
		DisableQEMU:   !cfg.QEMUEnabled,
	})
	eng.Warm()

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
	probe.HasImage = func(ctx context.Context, ref string) bool {
		ok, err := eng.HasImage(ctx, ref)
		return err == nil && ok
	}
	probe.DockerCheck = eng.HasDocker
	sbSvc := sandbox.NewService(store, eng, wsFS, cfg.DefaultImage, cfg.DefaultTTL, logger, sandbox.WithTemplates(tplSvc), sandbox.WithBrowser(browserHub))
	logger.Info("using multi backend", slog.String("default_agent_engine", cfg.Backend))
	// The hub dials the user's browser container from the control plane:
	// sandbox.Service.Dial authorizes via authz, so pass an internal actor.
	browserHub.SetDialer(internalDialer{sandbox: sbSvc})
	browserHub.SetTokenLookup(func(sandboxID string) string {
		// The hub runs inside the control plane: sandbox.Service.Get authorizes
		// via authz, so pass an internal admin actor for this metadata read.
		ctx := authz.WithActor(context.Background(), authz.Actor{Username: "roundpend", Admin: true})
		sb, err := sbSvc.Get(ctx, sandboxID)
		if err != nil || sb == nil || sb.Metadata == nil {
			return ""
		}
		return sb.Metadata["browserToken"]
	})
	return &backendSet{engine: eng, probe: probe, sandboxes: sbSvc}
}
