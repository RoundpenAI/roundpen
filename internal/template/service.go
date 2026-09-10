package template

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

// Service resolves template references and lists registered templates.
type Service struct {
	store          *Store
	builder        builder.Runner
	backend        string
	defaultImage   string
	fallbackLegacy bool
	logger         *slog.Logger

	buildMu sync.Mutex
	builds  map[string]struct{}

	assignDefaultMu sync.Mutex
	assignDefault   map[string]bool // buildID -> move default tag on ready
}

// NewService constructs a template service.
func NewService(store *Store, defaultImage string) *Service {
	return &Service{
		store:          store,
		defaultImage:   defaultImage,
		fallbackLegacy: true,
		logger:         slog.Default(),
		builds:         map[string]struct{}{},
		assignDefault:  map[string]bool{},
	}
}

// Seed ensures built-in templates exist.
func (s *Service) Seed(ctx context.Context, backend string) error {
	return s.store.SeedBuiltin(ctx, backend, s.defaultImage)
}

// List returns registered templates.
func (s *Service) List(ctx context.Context) ([]Record, error) {
	return s.store.List(ctx)
}

// Resolve maps a templateID string to image and resource limits.
// Unknown refs fall back to treating templateID as a raw image when fallbackLegacy is enabled.
func (s *Service) Resolve(ctx context.Context, templateID string) (Resolved, error) {
	ref := ParseRef(templateID)
	var (
		res Resolved
		err error
	)
	if ref.BuildID != "" {
		res, err = s.store.ResolveByBuildID(ctx, ref.BuildID)
	} else {
		res, err = s.store.ResolveByTag(ctx, ref)
	}
	if err == nil {
		res.RequestRef = templateID
		if res.Alias == "" {
			res.Alias = templateID
		}
		if strings.EqualFold(res.Slot, "browser") || strings.EqualFold(res.Profile, "browser") {
			res.UseImageCmd = true
			if res.Slot == "" {
				res.Slot = "browser"
			}
		}
		if res.Slot == "" {
			res.Slot = "agent"
		}
		return res, nil
	}
	if !s.fallbackLegacy || !errors.Is(err, ErrNotFound) {
		return Resolved{}, err
	}
	img := templateID
	if img == "" {
		img = s.defaultImage
	}
	return Resolved{
		RequestRef: templateID,
		Alias:      templateID,
		Image:      img,
		Profile:    "dev",
		Slot:       "agent",
		CPUCount:   1,
		MemoryMB:   512,
		DiskSizeMB: 5120,
	}, nil
}

// RecordSpawn updates template usage stats (best-effort).
func (s *Service) RecordSpawn(ctx context.Context, templateID string) {
	if templateID == "" {
		return
	}
	_ = s.store.RecordSpawn(ctx, templateID)
}

// Exists reports whether a name is registered in the default namespace.
func (s *Service) Exists(ctx context.Context, name string) (bool, error) {
	name = ParseRef(name).Name
	if !ValidateName(name) {
		return false, fmt.Errorf("invalid template name")
	}
	_, err := s.store.ResolveByTag(ctx, ParsedRef{Namespace: DefaultNamespace, Name: name, Tag: DefaultTag})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

// SetDefaultImage updates the fallback image for template resolution.
func (s *Service) SetDefaultImage(image string) {
	if strings.TrimSpace(image) != "" {
		s.defaultImage = strings.TrimSpace(image)
	}
}
