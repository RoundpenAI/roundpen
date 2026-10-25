package vcs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RemoteRepo is a repository the user can bind to a Project.
type RemoteRepo struct {
	FullName    string `json:"fullName"` // owner/name
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	CloneURL    string `json:"cloneUrl"`
	HTMLURL     string `json:"htmlUrl,omitempty"`
	Private     bool   `json:"private"`
	Description string `json:"description,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// ListGitHubRepos returns repositories visible to the token (owned + collab + org).
func ListGitHubRepos(ctx context.Context, client *http.Client, token string) ([]RemoteRepo, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("github token is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	var out []RemoteRepo
	page := 1
	for page <= 5 { // cap at 500 repos
		u := fmt.Sprintf(
			"https://api.github.com/user/repos?per_page=100&page=%d&sort=updated&affiliation=owner,collaborator,organization_member",
			page,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("github auth failed (%d)", resp.StatusCode)
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("github repos HTTP %d", resp.StatusCode)
		}
		var rows []struct {
			Name        string `json:"name"`
			FullName    string `json:"full_name"`
			CloneURL    string `json:"clone_url"`
			HTMLURL     string `json:"html_url"`
			Private     bool   `json:"private"`
			Description string `json:"description"`
			UpdatedAt   string `json:"updated_at"`
			Owner       struct {
				Login string `json:"login"`
			} `json:"owner"`
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("decode github repos: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			clone := strings.TrimSpace(r.CloneURL)
			if clone == "" && r.FullName != "" {
				clone = "https://github.com/" + r.FullName + ".git"
			}
			out = append(out, RemoteRepo{
				FullName:    r.FullName,
				Name:        r.Name,
				Owner:       r.Owner.Login,
				CloneURL:    clone,
				HTMLURL:     r.HTMLURL,
				Private:     r.Private,
				Description: r.Description,
				UpdatedAt:   r.UpdatedAt,
			})
		}
		if len(rows) < 100 {
			break
		}
		page++
	}
	return out, nil
}

// NormalizeCloneURL ensures a usable https clone URL for common shorthand.
func NormalizeCloneURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "git@") || strings.HasPrefix(s, "ssh://") || strings.HasPrefix(s, "git://") {
		return s
	}
	// owner/repo
	if !strings.Contains(s, "://") && strings.Count(s, "/") == 1 {
		return "https://github.com/" + s + ".git"
	}
	if u, err := url.Parse("https://" + s); err == nil && u.Host != "" {
		return "https://" + s
	}
	return s
}
