package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// RegisterIssues adds tracker tools. They call the control-plane API as the actor
// (like ListSessions), so validation and ownership live in one place — the HTTP layer.
// sessionID binds the registration-time chat session, the way BrowserBinder does, so
// created issues inherit their assistant and session server-side.
func RegisterIssues(r *Registry, api *RoundpenHTTP, sessionID string) {
	if r == nil || api == nil {
		return
	}
	r.Register(Tool{
		Name: "CreateIssue",
		Description: "Create a work item in the user's issue tracker and return its short key (ISS-12), " +
			"which is how you and the user refer to it later. Use it when the user starts a new piece of work — " +
			"not for a one-off question or a single trivial edit; if it is ambiguous whether they want an issue, ask. " +
			"Clarify scope, direction and decisions with AskUserQuestion before writing any document.",
		Parameters: objectSchema(map[string]any{
			"title":   map[string]any{"type": "string", "description": "Short imperative title"},
			"summary": map[string]any{"type": "string", "description": "One-line summary of the intended outcome"},
		}, "title"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Title   string `json:"title"`
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{"title": a.Title}
			if strings.TrimSpace(a.Summary) != "" {
				body["summary"] = a.Summary
			}
			if sessionID != "" {
				body["sessionId"] = sessionID
			}
			return api.do(ctx, actor, http.MethodPost, "/v1/issues", body)
		},
	})
	r.Register(Tool{
		Name: "ListIssues",
		Description: "List the user's issues, most recently updated first, with their status; pass status to " +
			"filter (drafting, specced, planned, in_progress, done, cancelled). Check this before starting new work " +
			"so you continue issues that are already in flight instead of duplicating them.",
		Parameters: objectSchema(map[string]any{
			"status": map[string]any{"type": "string", "description": "Optional status filter"},
		}),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			path := "/v1/issues"
			if s := strings.TrimSpace(a.Status); s != "" {
				path += "?status=" + url.QueryEscape(s)
			}
			return api.do(ctx, actor, http.MethodGet, path, nil)
		},
	})
	r.Register(Tool{
		Name: "GetIssue",
		Description: "Read one issue by short key (ISS-12): its fields, the spec/plan document index (DOC-n with " +
			"version and status) and the task checklist (TSK-n). Load it before acting — the server advances the " +
			"issue status as documents and tasks change, so a status you remember may be stale.",
		Parameters: objectSchema(map[string]any{
			"key": map[string]any{"type": "string", "description": "Issue key, e.g. ISS-12"},
		}, "key"),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			return api.do(ctx, actor, http.MethodGet, "/v1/issues/"+url.PathEscape(a.Key), nil)
		},
	})
	r.Register(Tool{
		Name: "UpdateIssue",
		Description: "Update an issue by key (ISS-12): status is one of drafting, specced, planned, in_progress, " +
			"done, cancelled; title and summary are also editable. The status usually advances on its own when a " +
			"current spec/plan is written or a task starts — set it explicitly to cancel the issue or to close it " +
			"early. Send only the fields you mean to change.",
		Parameters: objectSchema(map[string]any{
			"key":     map[string]any{"type": "string", "description": "Issue key, e.g. ISS-12"},
			"status":  map[string]any{"type": "string", "description": "drafting, specced, planned, in_progress, done or cancelled"},
			"title":   map[string]any{"type": "string"},
			"summary": map[string]any{"type": "string"},
		}, "key"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key     string  `json:"key"`
				Status  *string `json:"status"`
				Title   *string `json:"title"`
				Summary *string `json:"summary"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{}
			if a.Status != nil {
				body["status"] = *a.Status
			}
			if a.Title != nil {
				body["title"] = *a.Title
			}
			if a.Summary != nil {
				body["summary"] = *a.Summary
			}
			return api.do(ctx, actor, http.MethodPatch, "/v1/issues/"+url.PathEscape(a.Key), body)
		},
	})
	r.Register(Tool{
		Name: "WriteIssueDoc",
		Description: "Append a new version of an issue's spec or plan and return its key (DOC-88) and version. " +
			"Follow the workflow: clarify scope, direction and decisions first, then write the spec (kind=spec) " +
			"before the plan (kind=plan) — the plan must not invent scope the spec never agreed. Every write is a " +
			"new version, so revisions never overwrite history; status=current (the default) supersedes the previous " +
			"current version and advances the issue status (spec → specced, plan → planned).",
		Parameters: objectSchema(map[string]any{
			"key":       map[string]any{"type": "string", "description": "Issue key, e.g. ISS-12"},
			"kind":      map[string]any{"type": "string", "enum": []any{"spec", "plan"}},
			"contentMd": map[string]any{"type": "string", "description": "Document body in Markdown"},
			"title":     map[string]any{"type": "string", "description": "Optional document title"},
			"status":    map[string]any{"type": "string", "enum": []any{"draft", "current", "superseded"}, "description": "Defaults to current"},
			"taskKey":   map[string]any{"type": "string", "description": "Optional TSK-n this document belongs to"},
		}, "key", "kind", "contentMd"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key       string `json:"key"`
				Kind      string `json:"kind"`
				ContentMD string `json:"contentMd"`
				Title     string `json:"title"`
				Status    string `json:"status"`
				TaskKey   string `json:"taskKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{
				"kind":       a.Kind,
				"contentMd":  a.ContentMD,
				"authorType": "assistant",
			}
			if strings.TrimSpace(a.Title) != "" {
				body["title"] = a.Title
			}
			if strings.TrimSpace(a.Status) != "" {
				body["status"] = a.Status
			}
			if strings.TrimSpace(a.TaskKey) != "" {
				body["taskKey"] = a.TaskKey
			}
			if sessionID != "" {
				body["sessionId"] = sessionID
			}
			return api.do(ctx, actor, http.MethodPost, "/v1/issues/"+url.PathEscape(a.Key)+"/docs", body)
		},
	})
	r.Register(Tool{
		Name: "ReadIssueDoc",
		Description: "Read a document version of an issue by key. docKey is a DOC-n key, a uuid, or \"latest\" " +
			"(the current version, used when omitted). Read the current spec before writing the plan, and re-read " +
			"spec or plan whenever you need the exact agreed wording.",
		Parameters: objectSchema(map[string]any{
			"key":    map[string]any{"type": "string", "description": "Issue key, e.g. ISS-12"},
			"docKey": map[string]any{"type": "string", "description": "DOC-88, a uuid, or latest (default)"},
		}, "key"),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key    string `json:"key"`
				DocKey string `json:"docKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			docKey := strings.TrimSpace(a.DocKey)
			if docKey == "" {
				docKey = "latest"
			}
			return api.do(ctx, actor, http.MethodGet,
				"/v1/issues/"+url.PathEscape(a.Key)+"/docs/"+url.PathEscape(docKey), nil)
		},
	})
	r.Register(Tool{
		Name: "CreateTask",
		Description: "Add one task to an issue's checklist and return its key (TSK-34). Derive one task per plan " +
			"step — do not merge several steps into one — and pass planDocKey to record which plan version it came " +
			"from. Tasks append to the end unless position says otherwise; creating or starting a task moves the " +
			"issue to in_progress.",
		Parameters: objectSchema(map[string]any{
			"issueKey":   map[string]any{"type": "string", "description": "Issue key, e.g. ISS-12"},
			"title":      map[string]any{"type": "string", "description": "Short imperative title of one plan step"},
			"detail":     map[string]any{"type": "string", "description": "Optional notes: files, commands, acceptance criteria"},
			"position":   map[string]any{"type": "integer", "description": "Optional 1-based position; appends when omitted"},
			"planDocKey": map[string]any{"type": "string", "description": "Optional DOC-n of the plan this task derives from"},
		}, "issueKey", "title"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				IssueKey   string `json:"issueKey"`
				Title      string `json:"title"`
				Detail     string `json:"detail"`
				Position   int    `json:"position"`
				PlanDocKey string `json:"planDocKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{"title": a.Title}
			if strings.TrimSpace(a.Detail) != "" {
				body["detail"] = a.Detail
			}
			if a.Position > 0 {
				body["position"] = a.Position
			}
			if strings.TrimSpace(a.PlanDocKey) != "" {
				body["planDocKey"] = a.PlanDocKey
			}
			if sessionID != "" {
				body["sessionId"] = sessionID
			}
			return api.do(ctx, actor, http.MethodPost, "/v1/issues/"+url.PathEscape(a.IssueKey)+"/tasks", body)
		},
	})
	r.Register(Tool{
		Name: "UpdateTask",
		Description: "Update a task by key (TSK-34): status is todo, in_progress, done, blocked or cancelled; " +
			"title and detail are also editable. Keep status current while you implement — in_progress when you " +
			"start a step, done when you finish it, blocked when you cannot proceed — because the issue status " +
			"follows automatically (all tasks done closes it, reopening a task reopens it).",
		Parameters: objectSchema(map[string]any{
			"key":    map[string]any{"type": "string", "description": "Task key, e.g. TSK-34"},
			"status": map[string]any{"type": "string", "enum": []any{"todo", "in_progress", "done", "blocked", "cancelled"}},
			"title":  map[string]any{"type": "string"},
			"detail": map[string]any{"type": "string"},
		}, "key"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key    string  `json:"key"`
				Status *string `json:"status"`
				Title  *string `json:"title"`
				Detail *string `json:"detail"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{}
			if a.Status != nil {
				body["status"] = *a.Status
			}
			if a.Title != nil {
				body["title"] = *a.Title
			}
			if a.Detail != nil {
				body["detail"] = *a.Detail
			}
			if sessionID != "" {
				body["sessionId"] = sessionID
			}
			return api.do(ctx, actor, http.MethodPatch, "/v1/tasks/"+url.PathEscape(a.Key), body)
		},
	})
}
