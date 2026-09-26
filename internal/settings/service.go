package settings

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/audit"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/template"
)

// RuntimeDeps are subsystems updated on PUT /v1/admin/settings.
type RuntimeDeps struct {
	AllowPublicReg   func(bool)
	Sandbox          *sandbox.Service
	Templates        *template.Service
	Probe            *runtime.Probe
	ReconfigureLLMGW func(context.Context) error
	LlmgwMounted     bool
	// BrowserProfile resolves a browser item's CDP source ("" = the global
	// default). nil falls back to the process config.
	BrowserProfile func(itemID string) (browser.Profile, bool)
	// SetAgentImage publishes the Agent image override ("" = follow the agent
	// template) to the environment service.
	SetAgentImage func(image string)
}

// Service manages persisted app settings.
type Service struct {
	store *Store
	cfg   *config.Config
	deps  RuntimeDeps

	mu      sync.RWMutex
	current AppSettings
}

// NewService constructs a settings service.
func NewService(store *Store, cfg *config.Config, deps RuntimeDeps, current AppSettings) *Service {
	return &Service{store: store, cfg: cfg, deps: deps, current: current}
}

// Bootstrap seeds from env when empty, otherwise loads DB values into cfg.
func Bootstrap(ctx context.Context, store *Store, cfg *config.Config) (AppSettings, error) {
	exists, err := store.Exists(ctx)
	if err != nil {
		return AppSettings{}, err
	}
	if !exists {
		seed := FromConfig(cfg)
		if err := store.Upsert(ctx, seed); err != nil {
			return AppSettings{}, err
		}
		return seed, nil
	}
	got, err := store.Load(ctx, FromConfig(cfg))
	if err != nil {
		return AppSettings{}, err
	}
	if err := got.Validate(); err != nil {
		got = FromConfig(cfg)
		if err := store.Upsert(ctx, got); err != nil {
			return AppSettings{}, err
		}
	}
	if err := ApplyToConfig(&got, cfg); err != nil {
		return AppSettings{}, err
	}
	return got, nil
}

// Current returns the active settings snapshot.
func (s *Service) Current() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Update validates, persists, and hot-applies settings.
func (s *Service) Update(ctx context.Context, next AppSettings) error {
	prev := s.Current()
	next.MergeSecrets(prev)
	next.normalize()
	if err := next.Validate(); err != nil {
		return err
	}
	if err := ApplyToConfig(&next, s.cfg); err != nil {
		_ = ApplyToConfig(&prev, s.cfg)
		return err
	}
	if err := s.applyRuntime(ctx, next); err != nil {
		_ = ApplyToConfig(&prev, s.cfg)
		_ = s.applyRuntime(ctx, prev)
		return err
	}
	if err := s.store.Upsert(ctx, next); err != nil {
		return err
	}
	s.mu.Lock()
	s.current = next
	s.mu.Unlock()
	audit.Record(ctx, "settings.update")
	return nil
}

func (s *Service) applyRuntime(ctx context.Context, v AppSettings) error {
	if s.deps.AllowPublicReg != nil {
		s.deps.AllowPublicReg(v.AllowPublicRegistration)
	}
	if s.deps.Sandbox != nil {
		s.deps.Sandbox.SetDefaults(v.DefaultImage, time.Duration(v.DefaultTtlSeconds)*time.Second)
	}
	if s.deps.Templates != nil {
		s.deps.Templates.SetDefaultImage(v.DefaultImage)
	}
	if s.deps.SetAgentImage != nil {
		s.deps.SetAgentImage(v.AgentImage)
	}
	if s.deps.ReconfigureLLMGW != nil {
		if err := s.deps.ReconfigureLLMGW(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Response returns settings and system metadata for HTTP handlers.
func (s *Service) Response() (AppSettings, SystemInfo) {
	return s.Current().SanitizeForResponse(), s.System()
}

// System returns read-only infrastructure metadata.
func (s *Service) System() SystemInfo {
	info := SystemFromConfig(s.cfg, s.deps.LlmgwMounted)
	info.CDPProviderActive = s.browserProfile("").EffectiveProvider()
	return info
}

// BrowserTestResult is the /v1/admin/settings/browser/test payload.
type BrowserTestResult struct {
	Provider   string   `json:"provider"`
	Endpoint   string   `json:"endpoint,omitempty"`
	Path       string   `json:"path,omitempty"`
	Version    string   `json:"version,omitempty"`
	Playwright []string `json:"playwright,omitempty"`
	ChromePath string   `json:"chromePath,omitempty"`
	ChromeOK   bool     `json:"chromeOk,omitempty"`
}

// TestBrowser probes the browser source of an item ("" = the global default).
func (s *Service) TestBrowser(ctx context.Context, itemID string) (BrowserTestResult, error) {
	profile := s.browserProfile(itemID)
	res := BrowserTestResult{Provider: profile.EffectiveProvider()}
	switch res.Provider {
	case config.CDPProviderHost:
		res.ChromePath = browser.ChromePath()
		res.ChromeOK = res.ChromePath != ""
		if !res.ChromeOK {
			return res, fmt.Errorf("no Chrome binary found on this host")
		}
		return res, nil
	case config.CDPProviderRemote, config.CDPProviderCloud:
		res.Endpoint = profile.Endpoint
		probe, err := browser.ProbeCDP(ctx, profile.Endpoint, profile.Token)
		if probe != nil {
			res.Path = probe.Path
			res.Version = probe.Version
			res.Playwright = probe.Playwright
		}
		return res, err
	default: // docker / auto — Roundpen-managed container
		if s.deps.Probe != nil {
			if err := s.deps.Probe.RequireBrowser(profile); err != nil {
				return res, err
			}
		}
		port := profile.Port
		if port <= 0 {
			port = config.DefaultCDPPort
		}
		res.Endpoint = fmt.Sprintf("roundpen-managed container, CDP port %d", port)
		return res, nil
	}
}

// browserProfile resolves an item's browser source; a miss (or no resolver
// wired) falls back to the live process config, which settings hot-apply on
// PUT.
func (s *Service) browserProfile(itemID string) browser.Profile {
	if s.deps.BrowserProfile != nil {
		if p, ok := s.deps.BrowserProfile(itemID); ok {
			return p
		}
	}
	return browser.DefaultProfile(s.cfg)
}
