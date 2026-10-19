// Package project models Agent Projects: long-lived work containers that bind
// a local repo path and a coordinator assistant.
package project

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/workspace"
)

const (
	StatusActive   = "active"
	StatusArchived = "archived"
)

// Project is a product-level work container (not the projects/ directory alone).
type Project struct {
	ID                     string    `json:"id"`
	Key                    string    `json:"key"`
	UserID                 string    `json:"userId"`
	Name                   string    `json:"name"`
	Icon                   string    `json:"icon,omitempty"`
	Slug                   string    `json:"slug"`
	RepoPath               string    `json:"repoPath"`
	RepoURL                string    `json:"repoUrl,omitempty"`
	CoordinatorAssistantID string    `json:"coordinatorAssistantId,omitempty"`
	CoordinatorSessionID   string    `json:"coordinatorSessionId,omitempty"`
	Status                 string    `json:"status"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type CreateInput struct {
	Name    string
	Icon    string
	Slug    string // optional; derived from Name when empty
	RepoURL string // optional; GitHub remote not required
}

type UpdateInput struct {
	Name    *string
	Icon    *string
	RepoURL *string
	Status  *string
}

type ListFilter struct {
	Status string
	Limit  int
}

func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	if len([]rune(strings.TrimSpace(name))) > 120 {
		return fmt.Errorf("name is too long")
	}
	return nil
}

func ValidateStatus(status string) error {
	switch status {
	case StatusActive, StatusArchived:
		return nil
	default:
		return fmt.Errorf("status must be active or archived")
	}
}

func ValidateRepoURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// SCP-like git remotes: git@host:path/repo.git
	if strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("repoUrl is invalid")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ssh", "git":
		if u.Host == "" {
			return fmt.Errorf("repoUrl is invalid")
		}
		return nil
	case "":
		// host/path without scheme (e.g. github.com/org/repo)
		if strings.Contains(raw, "/") && !strings.Contains(raw, " ") {
			return nil
		}
		return fmt.Errorf("repoUrl is invalid")
	default:
		return fmt.Errorf("repoUrl scheme must be http(s), ssh, or git")
	}
}

// ResolveSlug returns a validated slug from an explicit value or the name.
func ResolveSlug(explicit, name string) (string, error) {
	slug := strings.TrimSpace(explicit)
	if slug == "" {
		slug = workspace.SlugifyProjectName(name)
	} else {
		slug = strings.ToLower(slug)
	}
	if !workspace.ValidProjectSlug(slug) {
		return "", fmt.Errorf("slug must match %s", workspace.ProjectSlugPattern.String())
	}
	return slug, nil
}
