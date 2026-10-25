package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// RegisterProjects adds Project delegate tools for coordinator sessions.
// defaultProjectKey (PRJ-n), when set, is used when the model omits projectKey.
func RegisterProjects(r *Registry, api *RoundpenHTTP, sessionID, defaultProjectKey string) {
	if r == nil || api == nil {
		return
	}
	defaultKey := strings.TrimSpace(defaultProjectKey)
	resolveKey := func(raw string) (string, error) {
		key := strings.TrimSpace(raw)
		if key == "" {
			key = defaultKey
		}
		if key == "" {
			return "", fmt.Errorf("projectKey is required (e.g. PRJ-1)")
		}
		return key, nil
	}
	keyDesc := "Project key, e.g. PRJ-1"
	if defaultKey != "" {
		keyDesc = "Project key (default " + defaultKey + " when omitted)"
	}

	r.Register(Tool{
		Name: "DelegateProjectWork",
		Description: "Delegate a software-dev task to a role-specific project worker. REQUIRED for " +
			"clone/fetch, installs, builds, tests, UI/frontend, APIs, and any other code or file changes — " +
			"you cannot run Bash or edit files yourself. Do not refuse with docs; call this tool instead. " +
			"Workers run one at a time (serial queue). Role: implementer (write/clone/run commands), " +
			"reviewer (review diffs, no edits), or tester (run/verify tests). " +
			"Split large requirements into several focused workers. Returns WRK-n and sessionId.",
		Parameters: objectSchema(map[string]any{
			"projectKey": map[string]any{"type": "string", "description": keyDesc},
			"title":      map[string]any{"type": "string", "description": "Short imperative worker title"},
			"brief":      map[string]any{"type": "string", "description": "Detailed instructions for the worker"},
			"role": map[string]any{
				"type":        "string",
				"description": "implementer | reviewer | tester (default implementer)",
				"enum":        []any{"implementer", "reviewer", "tester"},
			},
			"issueKey": map[string]any{"type": "string", "description": "Optional related issue key ISS-n"},
		}, "title"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				ProjectKey string `json:"projectKey"`
				Title      string `json:"title"`
				Brief      string `json:"brief"`
				Role       string `json:"role"`
				IssueKey   string `json:"issueKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			key, err := resolveKey(a.ProjectKey)
			if err != nil {
				return "", err
			}
			body := map[string]any{"title": a.Title}
			if strings.TrimSpace(a.Brief) != "" {
				body["brief"] = a.Brief
			}
			if strings.TrimSpace(a.Role) != "" {
				body["role"] = a.Role
			}
			if strings.TrimSpace(a.IssueKey) != "" {
				body["issueKey"] = a.IssueKey
			}
			path := "/v1/projects/" + url.PathEscape(key) + "/workers"
			return api.do(ctx, actor, http.MethodPost, path, body)
		},
	})
	r.Register(Tool{
		Name: "ListProjectWorkers",
		Description: "List worker agents for a project (WRK-n), newest first, with status queued/running/" +
			"succeeded/failed. Check before delegating more work so you do not pile up unnecessary tasks.",
		Parameters: objectSchema(map[string]any{
			"projectKey": map[string]any{"type": "string", "description": keyDesc},
		}),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				ProjectKey string `json:"projectKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			key, err := resolveKey(a.ProjectKey)
			if err != nil {
				return "", err
			}
			path := "/v1/projects/" + url.PathEscape(key) + "/workers"
			return api.do(ctx, actor, http.MethodGet, path, nil)
		},
	})
	r.Register(Tool{
		Name: "GetProject",
		Description: "Read one project by key (PRJ-n): name, repo path, status, coordinator ids. Use before " +
			"delegating so you know the workspace path workers should use.",
		Parameters: objectSchema(map[string]any{
			"projectKey": map[string]any{"type": "string", "description": keyDesc},
		}),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				ProjectKey string `json:"projectKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			key, err := resolveKey(a.ProjectKey)
			if err != nil {
				return "", err
			}
			return api.do(ctx, actor, http.MethodGet, "/v1/projects/"+url.PathEscape(key), nil)
		},
	})
	r.Register(Tool{
		Name: "ListProjects",
		Description: "List the user's active projects (key, name, repo path). Use when you are unsure of the " +
			"projectKey before delegating.",
		Parameters: objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			return api.do(ctx, actor, http.MethodGet, "/v1/projects?status=active", nil)
		},
	})
	_ = sessionID // reserved: future auto-resolve when defaultProjectKey is empty
}
