package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// RegisterRoutines adds standing-task tools for an ordinary chat. Run-only
// tools (FinishRun, RequestRoutineConfirm) are registered separately.
func RegisterRoutines(r *Registry, api *RoundpenHTTP, sessionID string) {
	if r == nil || api == nil {
		return
	}
	r.Register(Tool{
		Name: "CreateRoutine",
		Description: "Create a standing routine (RTN-n) that runs on a schedule without a new chat message. " +
			"Ask the user to confirm the brief, schedule, timezone, autonomy (read or browse), and assignee before calling. " +
			"The assignee defaults to you. To hand it to another assistant, AskUserQuestion first and pass assigneeConfirmed=true. " +
			"Do not create one for a one-off question.",
		Parameters: objectSchema(map[string]any{
			"title":               map[string]any{"type": "string"},
			"brief":               map[string]any{"type": "string", "description": "What to do on every run"},
			"cron":                map[string]any{"type": "string", "description": "Five-field cron, e.g. 0 8 * * 1"},
			"timezone":            map[string]any{"type": "string", "description": "IANA timezone, e.g. Asia/Shanghai"},
			"autonomy":            map[string]any{"type": "string", "enum": []string{"read", "browse"}},
			"hosts":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Bare hostnames. Required for browse, forbidden for read"},
			"deliverIm":           map[string]any{"type": "boolean"},
			"assigneeAssistantId": map[string]any{"type": "string"},
			"assigneeConfirmed":   map[string]any{"type": "boolean"},
			"issueKey":            map[string]any{"type": "string"},
		}, "title", "brief", "cron", "timezone", "autonomy"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Title               string   `json:"title"`
				Brief               string   `json:"brief"`
				Cron                string   `json:"cron"`
				Timezone            string   `json:"timezone"`
				Autonomy            string   `json:"autonomy"`
				Hosts               []string `json:"hosts"`
				DeliverIM           *bool    `json:"deliverIm"`
				AssigneeAssistantID string   `json:"assigneeAssistantId"`
				AssigneeConfirmed   bool     `json:"assigneeConfirmed"`
				IssueKey            string   `json:"issueKey"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{
				"title": a.Title, "brief": a.Brief, "cron": a.Cron,
				"timezone": a.Timezone, "autonomy": a.Autonomy,
			}
			if a.Hosts != nil {
				body["hosts"] = a.Hosts
			}
			if a.DeliverIM != nil {
				body["deliverIm"] = *a.DeliverIM
			}
			if a.AssigneeAssistantID != "" {
				body["assigneeAssistantId"] = a.AssigneeAssistantID
				body["assigneeConfirmed"] = a.AssigneeConfirmed
			}
			if a.IssueKey != "" {
				body["issueKey"] = a.IssueKey
			}
			if sessionID != "" {
				body["sessionId"] = sessionID
			}
			return api.do(ctx, actor, http.MethodPost, "/v1/routines", body)
		},
	})
	r.Register(Tool{
		Name:        "ListRoutines",
		Description: "List the user's standing routines, newest first. Pass status to filter (active, paused, archived).",
		Parameters: objectSchema(map[string]any{
			"status": map[string]any{"type": "string"},
		}),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			path := "/v1/routines"
			if s := strings.TrimSpace(a.Status); s != "" {
				path += "?status=" + url.QueryEscape(s)
			}
			return api.do(ctx, actor, http.MethodGet, path, nil)
		},
	})
	r.Register(Tool{
		Name:        "GetRoutine",
		Description: "Read one standing routine by RTN-n, including its state cursor and the latest run summary.",
		Parameters:  objectSchema(map[string]any{"key": map[string]any{"type": "string"}}, "key"),
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			return api.do(ctx, actor, http.MethodGet, "/v1/routines/"+url.PathEscape(a.Key), nil)
		},
	})
	r.Register(Tool{
		Name: "UpdateRoutine",
		Description: "Pause, resume, archive, reschedule, or reassign a standing routine. " +
			"Reassigning requires the user to have chosen the new assistant in this turn (assigneeConfirmed=true).",
		Parameters: objectSchema(map[string]any{
			"key":                 map[string]any{"type": "string"},
			"title":               map[string]any{"type": "string"},
			"brief":               map[string]any{"type": "string"},
			"cron":                map[string]any{"type": "string"},
			"timezone":            map[string]any{"type": "string"},
			"autonomy":            map[string]any{"type": "string"},
			"hosts":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"deliverIm":           map[string]any{"type": "boolean"},
			"status":              map[string]any{"type": "string", "enum": []string{"active", "paused", "archived"}},
			"assigneeAssistantId": map[string]any{"type": "string"},
			"assigneeConfirmed":   map[string]any{"type": "boolean"},
			"issueKey":            map[string]any{"type": "string"},
		}, "key"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a map[string]any
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			key, _ := a["key"].(string)
			delete(a, "key")
			return api.do(ctx, actor, http.MethodPatch, "/v1/routines/"+url.PathEscape(key), a)
		},
	})
	r.Register(Tool{
		Name: "UpdateRoutineState",
		Description: "Replace the cursor JSON for a routine you are assigned (seen ids, last covered date). " +
			"Keep it small. Do not store the report body here.",
		Parameters: objectSchema(map[string]any{
			"key":   map[string]any{"type": "string"},
			"state": map[string]any{"type": "object"},
		}, "key", "state"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Key   string          `json:"key"`
				State json.RawMessage `json:"state"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{"state": a.State}
			if sessionID != "" {
				body["sessionId"] = sessionID
			}
			return api.do(ctx, actor, http.MethodPut, "/v1/routines/"+url.PathEscape(a.Key)+"/state", body)
		},
	})
}

// RegisterRunTools adds the tools that exist only inside a scheduled run.
func RegisterRunTools(r *Registry, api *RoundpenHTTP, sessionID, runKey string) {
	if r == nil || api == nil || runKey == "" {
		return
	}
	r.Register(Tool{
		Name: "FinishRun",
		Description: "Finish this scheduled run. Call it once, with a short summary the user will see, " +
			"after the work is done. artifacts may list workspace paths or markdown.",
		Parameters: objectSchema(map[string]any{
			"summary": map[string]any{"type": "string"},
			"artifacts": map[string]any{"type": "array", "items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":     map[string]any{"type": "string"},
					"markdown": map[string]any{"type": "string"},
				},
			}},
		}, "summary"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Summary   string `json:"summary"`
				Artifacts []any  `json:"artifacts"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			body := map[string]any{"summary": a.Summary, "sessionId": sessionID}
			if a.Artifacts != nil {
				body["artifacts"] = a.Artifacts
			}
			return api.do(ctx, actor, http.MethodPost, "/v1/routines/runs/"+url.PathEscape(runKey)+"/finish", body)
		},
	})
	r.Register(Tool{
		Name: "RequestRoutineConfirm",
		Description: "Stop this run and ask the user before an irreversible action: payment, final form submission, " +
			"deleting the user's data, or messaging anyone except the routine's delivery channel. " +
			"After it returns, do not continue the action.",
		Parameters: objectSchema(map[string]any{
			"title":    map[string]any{"type": "string"},
			"reason":   map[string]any{"type": "string"},
			"askHuman": map[string]any{"type": "string", "description": "What the user should do"},
		}, "title", "reason", "askHuman"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Title    string `json:"title"`
				Reason   string `json:"reason"`
				AskHuman string `json:"askHuman"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			return api.do(ctx, actor, http.MethodPost, "/v1/routines/runs/"+url.PathEscape(runKey)+"/confirm", map[string]any{
				"sessionId": sessionID, "title": a.Title, "reason": a.Reason, "askHuman": a.AskHuman,
			})
		},
	})
}
