package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// RegisterIssues adds tracker tools. They call the control-plane API as the actor
// (like ListSessions), so validation and ownership live in one place — the HTTP layer.
// sessionID binds the registration-time chat session, the way BrowserBinder does, so
// created issues inherit their assistant and session server-side.
func RegisterIssues(r *Registry, api *RoundpenHTTP, sessionID, assistantID string) {
	if r == nil || api == nil {
		return
	}
	asstID := strings.TrimSpace(assistantID)
	r.Register(Tool{
		Name: "CreateIssue",
		Description: "Create a work item in the user's issue tracker and return its short key (ISS-12), " +
			"which is how you and the user refer to it later. Use it when the user starts a new piece of work — " +
			"not for a one-off question or a single trivial edit; if it is ambiguous whether they want an issue, ask. " +
			"Clarify scope, direction and decisions with AskUserQuestion before writing any document.",
		Parameters: objectSchema(map[string]any{
			"title":     map[string]any{"type": "string", "description": "Short imperative title"},
			"summary":   map[string]any{"type": "string", "description": "One-line summary of the intended outcome"},
			"projectId": map[string]any{"type": "string", "description": "Optional project uuid to scope this issue"},
		}, "title"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Title     string `json:"title"`
				Summary   string `json:"summary"`
				ProjectID string `json:"projectId"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{"title": a.Title}
			if strings.TrimSpace(a.Summary) != "" {
				body["summary"] = a.Summary
			}
			if strings.TrimSpace(a.ProjectID) != "" {
				body["projectId"] = a.ProjectID
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
			"filter (new, spec_review, needs_discussion, triaged, claimed, running, verified, needs_review, " +
			"failed, closed, cancelled). Optionally pass projectId to list only issues for one project. " +
			"Check this before starting new work so you continue issues that are already in flight instead of " +
			"duplicating them. Prefer ListIssueBoard when you need the Kanban columns.",
		Parameters: objectSchema(map[string]any{
			"status":    map[string]any{"type": "string", "description": "Optional status filter"},
			"projectId": map[string]any{"type": "string", "description": "Optional project uuid filter"},
		}),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Status    string `json:"status"`
				ProjectID string `json:"projectId"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			q := url.Values{}
			if s := strings.TrimSpace(a.Status); s != "" {
				q.Set("status", s)
			}
			if p := strings.TrimSpace(a.ProjectID); p != "" {
				q.Set("projectId", p)
			}
			path := "/v1/issues"
			if enc := q.Encode(); enc != "" {
				path += "?" + enc
			}
			return api.do(ctx, actor, http.MethodGet, path, nil)
		},
	})
	r.Register(Tool{
		Name: "ListIssueBoard",
		Description: "Return the issue Kanban board: columns in workflow order with issues in each. " +
			"Use this when scanning or prioritizing work across statuses. Optional projectId scopes to one project.",
		Parameters: objectSchema(map[string]any{
			"projectId": map[string]any{"type": "string", "description": "Optional project uuid filter"},
		}),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				ProjectID string `json:"projectId"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			path := "/v1/issues/board"
			if p := strings.TrimSpace(a.ProjectID); p != "" {
				path += "?projectId=" + url.QueryEscape(p)
			}
			return api.do(ctx, actor, http.MethodGet, path, nil)
		},
	})
	r.Register(Tool{
		Name: "TransitionIssue",
		Description: "Move an issue along an allowed Kanban edge (e.g. triaged→claimed, running→verified). " +
			"Illegal jumps and thrash (discussion/review flip-flops, non-owner moves, missing reason) are rejected. " +
			"Always pass reason (≥8 chars). Pass evidence when reversing a recent move. " +
			"Only the claim owner (or sysadmin) may transition claimed/running issues. " +
			"After discussion_round≥3 the only agent path out of spec_review/needs_discussion is triaged.",
		Parameters: objectSchema(map[string]any{
			"key":      map[string]any{"type": "string", "description": "Issue key, e.g. ISS-12"},
			"to":       map[string]any{"type": "string", "description": "Target status"},
			"reason":   map[string]any{"type": "string", "description": "Required rationale (≥8 characters)"},
			"evidence": map[string]any{"type": "string", "description": "New evidence to break hysteresis (PR SHA, user decision, …)"},
		}, "key", "to", "reason"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key      string `json:"key"`
				To       string `json:"to"`
				Reason   string `json:"reason"`
				Evidence string `json:"evidence"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{
				"to": a.To, "reason": a.Reason, "actorKind": "agent",
			}
			if asstID != "" {
				body["assistantId"] = asstID
				body["actorId"] = asstID
			}
			if strings.TrimSpace(a.Evidence) != "" {
				body["evidence"] = a.Evidence
			}
			return api.do(ctx, actor, http.MethodPost,
				"/v1/issues/"+url.PathEscape(a.Key)+"/transitions", body)
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
		Description: "Update an issue by key (ISS-12): title, summary, optional prUrl (GitHub PR link). " +
			"Do not change status here — use TransitionIssue so anti-thrash (owner, rounds, reason) is enforced. " +
			"Remote PR discussion stays on GitHub — prUrl is only a pointer. Send only the fields you mean to change.",
		Parameters: objectSchema(map[string]any{
			"key":     map[string]any{"type": "string", "description": "Issue key, e.g. ISS-12"},
			"title":   map[string]any{"type": "string"},
			"summary": map[string]any{"type": "string"},
			"prUrl":   map[string]any{"type": "string", "description": "GitHub pull request URL after CreatePullRequest"},
		}, "key"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key     string  `json:"key"`
				Title   *string `json:"title"`
				Summary *string `json:"summary"`
				PRURL   *string `json:"prUrl"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{}
			if a.Title != nil {
				body["title"] = *a.Title
			}
			if a.Summary != nil {
				body["summary"] = *a.Summary
			}
			if a.PRURL != nil {
				body["prUrl"] = *a.PRURL
			}
			if len(body) == 0 {
				return "", fmt.Errorf("nothing to update; for status changes use TransitionIssue")
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
			"current version and advances the issue status (spec → spec_review, plan → triaged).",
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
			"issue to running.",
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
			"follows automatically (all tasks done → verified; reopening a task → running).",
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
