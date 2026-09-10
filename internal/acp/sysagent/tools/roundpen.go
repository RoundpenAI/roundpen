package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RoundpenHTTP calls Roundpen control-plane APIs as the actor (X-API-Key).
type RoundpenHTTP struct {
	BaseURL    string // e.g. http://127.0.0.1:19001
	HTTPClient *http.Client
}

func (c *RoundpenHTTP) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *RoundpenHTTP) do(ctx context.Context, actor Actor, method, path string, body any) (string, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return "", err
	}
	if actor.APIKey != "" {
		req.Header.Set("X-API-Key", actor.APIKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if len(raw) == 0 {
		return fmt.Sprintf(`{"ok":true,"status":%d}`, resp.StatusCode), nil
	}
	return string(raw), nil
}

// RegisterRoundpen adds Roundpen management tools.
func RegisterRoundpen(r *Registry, api *RoundpenHTTP) {
	if api == nil || r == nil {
		return
	}
	r.Register(Tool{
		Name: "roundpen_list_environments",
		Description: "List the current user's fixed environments (agent/browser slots). " +
			"status=absent on the agent slot means it is not started yet — call roundpen_ensure_agent or sandbox_exec. " +
			"Do not treat a running Browser as the only available environment.",
		Parameters: objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			raw, err := api.do(ctx, actor, http.MethodGet, "/v1/me/environments", nil)
			if err != nil {
				return "", err
			}
			return annotateEnvList(raw), nil
		},
	})
	r.Register(Tool{
		Name:        "roundpen_ensure_agent",
		Description: "Start or resume the Cloud Agent sandbox (git, shell, builds). Call this when agent status is absent or stopped. Browser cannot run shell.",
		Mutating:    true,
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			return api.do(ctx, actor, http.MethodPost, "/v1/me/environments/agent/ensure", nil)
		},
	})
	r.Register(Tool{
		Name:        "roundpen_ensure_browser",
		Description: "Ensure the user's Browser environment is running (Chrome / CDP). Not for git or shell.",
		Mutating:    true,
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			return api.do(ctx, actor, http.MethodPost, "/v1/me/environments/browser/ensure", nil)
		},
	})
	r.Register(Tool{
		Name:        "roundpen_list_templates",
		Description: "List available environment image templates (agent/browser slots).",
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			return api.do(ctx, actor, http.MethodGet, "/v1/templates", nil)
		},
	})
	r.Register(Tool{
		Name:        "roundpen_list_agent_sessions",
		Description: "List the current user's agent chat sessions.",
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			return api.do(ctx, actor, http.MethodGet, "/v1/agent-sessions", nil)
		},
	})
	r.Register(Tool{
		Name:        "roundpen_get_settings",
		Description: "Get admin app settings (admin only).",
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			if actor.Role != "admin" {
				return "", fmt.Errorf("admin role required")
			}
			return api.do(ctx, actor, http.MethodGet, "/v1/admin/settings", nil)
		},
	})
}

const envListNote = "agent status=absent means not started yet — call roundpen_ensure_agent or sandbox_exec. Browser cannot run git or shell."

func annotateEnvList(raw string) string {
	var wrap map[string]any
	if err := json.Unmarshal([]byte(raw), &wrap); err != nil || wrap == nil {
		return raw
	}
	wrap["note"] = envListNote
	out, err := json.Marshal(wrap)
	if err != nil {
		return raw
	}
	return string(out)
}
