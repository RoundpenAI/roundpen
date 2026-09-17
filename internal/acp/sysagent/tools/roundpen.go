package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// EnvLister lists fixed per-user environments (agent/browser slots).
type EnvLister interface {
	List(ctx context.Context, userID string) ([]userenv.EnvView, error)
}

// TemplateLister lists environment image templates.
type TemplateLister interface {
	List(ctx context.Context) ([]template.Record, error)
}

// SessionLister lists agent chat sessions for a user.
type SessionLister interface {
	ListByUser(ctx context.Context, userID string, limit int) ([]*agentsession.Session, error)
}

// SettingsSource returns sanitized admin settings (same shape as HTTP GET).
type SettingsSource interface {
	Response() (settings.AppSettings, settings.SystemInfo)
}

// RoundpenBinder calls control-plane services in-process as the tool Actor.
// Prefer this over HTTP + API keys: same process, permissions follow Actor.Username/Role.
type RoundpenBinder struct {
	Envs      EnvLister
	Templates TemplateLister
	Sessions  SessionLister
	Settings  SettingsSource
}

// RegisterRoundpen adds Roundpen management tools (model-facing names, no ensure tools).
func RegisterRoundpen(r *Registry, b *RoundpenBinder) {
	if b == nil || r == nil {
		return
	}
	r.Register(Tool{
		Name: "ListEnvironments",
		Description: "List the current user's fixed environments (agent/browser slots). " +
			"status=absent on the agent slot means it is not started yet — call Bash or a file tool. " +
			"Do not treat a running Browser as the only available environment.",
		Parameters: objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			if b.Envs == nil {
				return "", fmt.Errorf("environments not configured")
			}
			if strings.TrimSpace(actor.Username) == "" {
				return "", fmt.Errorf("actor username required")
			}
			list, err := b.Envs.List(ctx, actor.Username)
			if err != nil {
				return "", err
			}
			return scrubEnvViews(list), nil
		},
	})
	r.Register(Tool{
		Name:        "ListTemplates",
		Description: "List available environment image templates (agent/browser slots).",
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			if b.Templates == nil {
				return "[]", nil
			}
			list, err := b.Templates.List(ctx)
			if err != nil {
				return "", err
			}
			out := make([]map[string]any, 0, len(list))
			for _, rec := range list {
				out = append(out, templateSummary(rec))
			}
			raw, err := json.Marshal(out)
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	})
	r.Register(Tool{
		Name:        "ListSessions",
		Description: "List the current user's agent chat sessions.",
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			if b.Sessions == nil {
				return `{"sessions":[]}`, nil
			}
			if strings.TrimSpace(actor.Username) == "" {
				return "", fmt.Errorf("actor username required")
			}
			list, err := b.Sessions.ListByUser(ctx, actor.Username, 50)
			if err != nil {
				return "", err
			}
			if list == nil {
				list = []*agentsession.Session{}
			}
			// Strip sandbox ids from the model-facing payload.
			type sessView struct {
				ID          string    `json:"id"`
				UserID      string    `json:"userId"`
				Title       string    `json:"title"`
				ProviderID  string    `json:"providerId"`
				AssistantID string    `json:"assistantId,omitempty"`
				Status      string    `json:"status"`
				CreatedAt   time.Time `json:"createdAt"`
				UpdatedAt   time.Time `json:"updatedAt"`
			}
			views := make([]sessView, 0, len(list))
			for _, s := range list {
				views = append(views, sessView{
					ID: s.ID, UserID: s.UserID, Title: s.Title, ProviderID: s.ProviderID,
					AssistantID: s.AssistantID, Status: s.Status,
					CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
				})
			}
			raw, err := json.Marshal(map[string]any{"sessions": views})
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	})
	r.Register(Tool{
		Name:        "GetSettings",
		Description: "Get admin app settings (admin only).",
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			if actor.Role != "admin" {
				return "", fmt.Errorf("admin role required")
			}
			if b.Settings == nil {
				return "", fmt.Errorf("settings not configured")
			}
			cfg, sys := b.Settings.Response()
			raw, err := json.Marshal(map[string]any{"settings": cfg, "system": sys})
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	})
}

const envListNote = "agent status=absent means not started yet — call Bash or file tools (Read/Write/Edit). Browser cannot run git or shell."

func scrubEnvViews(list []userenv.EnvView) string {
	type view struct {
		Slot       string `json:"slot"`
		TemplateID string `json:"templateId,omitempty"`
		Status     string `json:"status"`
		Name       string `json:"name,omitempty"`
		Provider   string `json:"provider,omitempty"`
		Image      string `json:"image,omitempty"`
	}
	out := make([]view, 0, len(list))
	for _, e := range list {
		out = append(out, view{
			Slot: e.Slot, TemplateID: e.TemplateID, Status: e.Status,
			Name: e.Name, Provider: e.Provider, Image: e.Image,
		})
	}
	raw, err := json.Marshal(map[string]any{"environments": out, "note": envListNote})
	if err != nil {
		return `{"environments":[],"note":"` + envListNote + `"}`
	}
	return string(raw)
}

func templateSummary(rec template.Record) map[string]any {
	m := map[string]any{
		"templateID":  rec.TemplateID,
		"name":        rec.Name,
		"description": rec.Description,
		"slot":        rec.Slot,
		"profile":     rec.Profile,
		"public":      rec.Public,
		"buildStatus": string(rec.BuildStatus),
		"aliases":     rec.Aliases,
		"names":       rec.Names,
	}
	return m
}
