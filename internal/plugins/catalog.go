// Package plugins catalogs optional instance capabilities that stay out of
// the default console until an administrator enables them.
package plugins

import "strings"

const (
	// SelfHostedGit unlocks OAuth login for Gitea, GitLab and GitHub
	// Enterprise. GitHub.com stays a built-in path and does not need this.
	SelfHostedGit = "self-hosted-git"
)

// Plugin is one optional capability shown on the admin plugins page.
type Plugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Group       string `json:"group"`
}

var catalog = []Plugin{
	{
		ID:    SelfHostedGit,
		Name:  "Self-hosted Git",
		Group: "access",
		Description: "Gitea, GitLab and GitHub Enterprise OAuth login. " +
			"Enable only if this instance talks to a private Git host. " +
			"GitHub.com does not need this plugin.",
	},
}

var catalogByID = func() map[string]Plugin {
	m := make(map[string]Plugin, len(catalog))
	for _, p := range catalog {
		m[p.ID] = p
	}
	return m
}()

// Catalog returns the optional plugins in display order.
func Catalog() []Plugin {
	out := make([]Plugin, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup finds a catalog entry by id.
func Lookup(id string) (Plugin, bool) {
	p, ok := catalogByID[strings.ToLower(strings.TrimSpace(id))]
	return p, ok
}

// Sanitize keeps known plugin ids, lowercased and de-duplicated.
func Sanitize(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if _, ok := catalogByID[id]; !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// Enabled reports whether id is in the enabled set.
func Enabled(ids []string, id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, e := range ids {
		if strings.EqualFold(e, id) {
			return true
		}
	}
	return false
}
