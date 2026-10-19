package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// RegisterProjects adds Project delegate tools for coordinator sessions.
func RegisterProjects(r *Registry, api *RoundpenHTTP, sessionID string) {
	if r == nil || api == nil {
		return
	}
	r.Register(Tool{
		Name: "DelegateProjectWork",
		Description: "Delegate an implementation task to a project worker agent. Use this when you are the " +
			"project coordinator and need code or investigation done in the project repo. Workers run one at a " +
			"time (serial queue). Pass projectKey (PRJ-n). Returns a worker key (WRK-n) and sessionId you can " +
			"point the user at. Do not implement large coding tasks yourself — delegate them.",
		Parameters: objectSchema(map[string]any{
			"projectKey": map[string]any{"type": "string", "description": "Project key, e.g. PRJ-1"},
			"title":      map[string]any{"type": "string", "description": "Short imperative worker title"},
			"brief":      map[string]any{"type": "string", "description": "Detailed instructions for the worker"},
			"issueKey":   map[string]any{"type": "string", "description": "Optional related issue key ISS-n"},
		}, "projectKey", "title"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				ProjectKey string `json:"projectKey"`
				Title      string `json:"title"`
				Brief      string `json:"brief"`
				IssueKey   string `json:"issueKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{"title": a.Title}
			if strings.TrimSpace(a.Brief) != "" {
				body["brief"] = a.Brief
			}
			if strings.TrimSpace(a.IssueKey) != "" {
				body["issueKey"] = a.IssueKey
			}
			path := "/v1/projects/" + url.PathEscape(strings.TrimSpace(a.ProjectKey)) + "/workers"
			return api.do(ctx, actor, http.MethodPost, path, body)
		},
	})
	r.Register(Tool{
		Name: "ListProjectWorkers",
		Description: "List worker agents for a project (WRK-n), newest first, with status queued/running/" +
			"succeeded/failed. Check before delegating more work so you do not pile up unnecessary tasks.",
		Parameters: objectSchema(map[string]any{
			"projectKey": map[string]any{"type": "string", "description": "Project key, e.g. PRJ-1"},
		}, "projectKey"),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				ProjectKey string `json:"projectKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			path := "/v1/projects/" + url.PathEscape(strings.TrimSpace(a.ProjectKey)) + "/workers"
			return api.do(ctx, actor, http.MethodGet, path, nil)
		},
	})
	r.Register(Tool{
		Name: "GetProject",
		Description: "Read one project by key (PRJ-n): name, repo path, status, coordinator ids. Use before " +
			"delegating so you know the workspace path workers should use.",
		Parameters: objectSchema(map[string]any{
			"projectKey": map[string]any{"type": "string", "description": "Project key, e.g. PRJ-1"},
		}, "projectKey"),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				ProjectKey string `json:"projectKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			return api.do(ctx, actor, http.MethodGet, "/v1/projects/"+url.PathEscape(strings.TrimSpace(a.ProjectKey)), nil)
		},
	})
	_ = sessionID // reserved: future auto-resolve project from coordinator session
}
