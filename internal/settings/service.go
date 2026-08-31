package settings

import (
	"context"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/template"
)

// RuntimeDeps are subsystems updated on PUT /v1/admin/settings.
type RuntimeDeps struct {
	AllowPublicReg  func(bool)
	PreviewTokens   *preview.Store
	PreviewHandler  *preview.Handler
	Sandbox         *sandbox.Service
	Templates       *template.Service
	ReattachBuilder func() error
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
	got, err := store.Get(ctx)
	if err != nil {
		return AppSettings{}, err
	}
	if err := got.Validate(); err != nil {
		got = FromConfig(cfg)
		if err := store.Upsert(ctx, got); err != nil {
			return AppSettings{}, err
		}
	}
	ApplyToConfig(&got, cfg)
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
	if err := next.Validate(); err != nil {
		return err
	}
	if err := s.store.Upsert(ctx, next); err != nil {
		return err
	}
	ApplyToConfig(&next, s.cfg)
	s.mu.Lock()
	s.current = next
	s.mu.Unlock()
	s.applyRuntime(next)
	return nil
}

func (s *Service) applyRuntime(v AppSettings) {
	if s.deps.AllowPublicReg != nil {
		s.deps.AllowPublicReg(v.AllowPublicRegistration)
	}
	if s.deps.PreviewTokens != nil {
		s.deps.PreviewTokens.SetTTL(time.Duration(v.PreviewTokenTtlSeconds) * time.Second)
	}
	if s.deps.PreviewHandler != nil {
		s.deps.PreviewHandler.PublicURL = v.PreviewPublicURL
	}
	if s.deps.Sandbox != nil {
		s.deps.Sandbox.SetDefaults(v.DefaultImage, time.Duration(v.DefaultTtlSeconds)*time.Second)
	}
	if s.deps.Templates != nil {
		s.deps.Templates.SetDefaultImage(v.DefaultImage)
	}
	if s.deps.ReattachBuilder != nil {
		_ = s.deps.ReattachBuilder()
	}
}

// System returns read-only infrastructure metadata.
func (s *Service) System() SystemInfo {
	return SystemFromConfig(s.cfg)
}
