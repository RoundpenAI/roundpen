package template

import (
	"context"
	"errors"
	"strings"
)

// Service resolves template references and lists registered templates.
type Service struct {
	store          *Store
	defaultImage   string
	fallbackLegacy bool
}

// NewService constructs a template service.
func NewService(store *Store, defaultImage string) *Service {
	return &Service{
		store:          store,
		defaultImage:   defaultImage,
		fallbackLegacy: true,
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
// Unknown refs fall back to treating templateID as a raw image when
// fallbackLegacy is enabled. Because that fallback pulls and runs an
// arbitrary OCI image, it must only be reachable by admin-controlled
// inputs (config defaults, admin-gated HTTP routes).
func (s *Service) Resolve(ctx context.Context, templateID string) (Resolved, error) {
	ref := ParseRef(templateID)
	res, err := s.store.ResolveByName(ctx, ref)
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

// SetDefaultImage updates the fallback image for template resolution.
func (s *Service) SetDefaultImage(image string) {
	if strings.TrimSpace(image) != "" {
		s.defaultImage = strings.TrimSpace(image)
	}
}
