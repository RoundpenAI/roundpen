package plugins

import (
	"context"
	"fmt"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

// settingsAPI is the slice of the settings service plugins persist through.
type settingsAPI interface {
	Current() settings.AppSettings
	Update(ctx context.Context, next settings.AppSettings) error
}

// Service reads and writes the enabled plugin set.
type Service struct {
	Settings settingsAPI
}

// Status is a catalog entry plus whether it is on for this instance.
type Status struct {
	Plugin
	Enabled bool `json:"enabled"`
}

// List returns every catalog plugin with its enabled flag.
func (s *Service) List() []Status {
	ids := s.enabledIDs()
	out := make([]Status, 0, len(catalog))
	for _, p := range Catalog() {
		out = append(out, Status{Plugin: p, Enabled: Enabled(ids, p.ID)})
	}
	return out
}

// IsEnabled reports whether a catalog plugin is on.
func (s *Service) IsEnabled(id string) bool {
	return Enabled(s.enabledIDs(), id)
}

// SetEnabled turns a catalog plugin on or off.
func (s *Service) SetEnabled(ctx context.Context, id string, on bool) error {
	if _, ok := Lookup(id); !ok {
		return fmt.Errorf("unknown plugin %q", id)
	}
	if s == nil || s.Settings == nil {
		return fmt.Errorf("plugins store not configured")
	}
	cur := s.Settings.Current()
	next := map[string]struct{}{}
	for _, e := range Sanitize(cur.EnabledPlugins) {
		next[e] = struct{}{}
	}
	id = Sanitize([]string{id})[0]
	if on {
		next[id] = struct{}{}
	} else {
		delete(next, id)
	}
	cur.EnabledPlugins = keys(next)
	return s.Settings.Update(ctx, cur)
}

func (s *Service) enabledIDs() []string {
	if s == nil || s.Settings == nil {
		return nil
	}
	return Sanitize(s.Settings.Current().EnabledPlugins)
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return Sanitize(out)
}
