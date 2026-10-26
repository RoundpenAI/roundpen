package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/audit"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// Expert states for ListExperts / DelegateToExpert (validation contract).
const (
	ExpertReady       = "ready"
	ExpertNeedsAuth   = "needs_auth"
	ExpertUnavailable = "unavailable"
	ExpertBusy        = "busy"
)

// ExpertView is the model-facing specialist descriptor.
type ExpertView struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"displayName"`
	Capabilities []string `json:"capabilities"`
	State        string   `json:"state"`
	Reason       string   `json:"reason,omitempty"`
	DeepLink     string   `json:"deepLink,omitempty"`
	Command      string   `json:"command,omitempty"`
}

// ExpertBinder discovers coding ACP specialists and records one-shot delegate stubs.
type ExpertBinder struct {
	Envs        EnvLister
	Providers   []providers.Provider // nil → providers.Default()
	SessionID   string
	AssistantID string
}

func (b *ExpertBinder) catalog() []providers.Provider {
	if b != nil && len(b.Providers) > 0 {
		return b.Providers
	}
	return providers.Default()
}

// RegisterExperts adds ListExperts (read) and DelegateToExpert (mutating stub).
func RegisterExperts(r *Registry, b *ExpertBinder) {
	if r == nil || b == nil {
		return
	}
	r.Register(Tool{
		Name: "ListExperts",
		Description: "List coding specialist agents available for one-shot delegation (Claude Code, Codex, …). " +
			"Returns id, displayName, state (ready|needs_auth|unavailable|busy), reason, deepLink. " +
			"Call this BEFORE DelegateToExpert. Do not invent expert ids that are not listed. " +
			"MCP connectors are unrelated — use CallMcpTool for those.",
		Parameters: objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			views, err := b.list(ctx, actor)
			if err != nil {
				return "", err
			}
			actx := authz.WithActor(ctx, actor.Authz())
			audit.Record(actx, "expert.list",
				"session_id", b.SessionID,
				"assistant_id", b.AssistantID,
				"count", len(views),
			)
			raw, err := json.Marshal(map[string]any{
				"experts": views,
				"note":    "Call ListExperts before DelegateToExpert. Delegation is one-shot; default handoff is not a sticky expert chat.",
			})
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	})
	r.Register(Tool{
		Name: "DelegateToExpert",
		Description: "Delegate a one-shot coding task to a specialist ACP agent in the Docker agent environment. " +
			"ALWAYS call ListExperts first and only use a ready expert id. " +
			"Omit expertId to auto-route to the first ready expert (audit mode=auto). " +
			"Pass goal plus optional paths. Phase-0 may return outcome=stubbed until AttachExec coding loop ships — " +
			"still use this tool so routing and audit can be validated.",
		Parameters: objectSchema(map[string]any{
			"expertId": map[string]any{
				"type":        "string",
				"description": "Specialist id from ListExperts (e.g. claude, codex). Omit for auto-route.",
			},
			"goal": map[string]any{
				"type":        "string",
				"description": "One-shot task goal for the specialist",
			},
			"paths": map[string]any{
				"type":        "array",
				"description": "Optional relative paths under /workspace",
				"items":       map[string]any{"type": "string"},
			},
		}, "goal"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			return b.delegate(ctx, actor, raw)
		},
	})
}

func (b *ExpertBinder) list(ctx context.Context, actor Actor) ([]ExpertView, error) {
	if strings.TrimSpace(actor.Username) == "" {
		return nil, fmt.Errorf("actor username required")
	}
	var envs []userenv.EnvView
	if b.Envs != nil {
		list, err := b.Envs.List(ctx, actor.Username)
		if err != nil {
			return nil, err
		}
		envs = list
	}
	deep := "/settings?section=git"
	if aid := strings.TrimSpace(b.AssistantID); aid != "" {
		deep = "/a/" + aid + "?tab=experts"
	}
	out := make([]ExpertView, 0)
	for _, p := range providers.CodingExperts(b.catalog()) {
		state, reason := classifyExpert(p, envs)
		caps := []string{"code_edit", "shell"}
		if p.ID == "claude" {
			caps = append(caps, "pr_assist")
		}
		out = append(out, ExpertView{
			ID:           p.ID,
			DisplayName:  p.Name,
			Capabilities: caps,
			State:        state,
			Reason:       reason,
			DeepLink:     deep,
			Command:      p.Command,
		})
	}
	return out, nil
}

func classifyExpert(p providers.Provider, envs []userenv.EnvView) (state, reason string) {
	if !p.Enabled {
		return ExpertUnavailable, "disabled"
	}
	agentStatus := "absent"
	for _, e := range envs {
		if e.Slot == userenv.SlotAgent {
			agentStatus = e.Status
			break
		}
	}
	switch agentStatus {
	case "missing":
		return ExpertUnavailable, "agent_slot_missing"
	default:
		// absent/running/starting: EnsureAgent can bring the slot up.
		// Vendor own-login probing lands in a later phase → Phase 0 marks ready.
		return ExpertReady, ""
	}
}

func (b *ExpertBinder) delegate(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
	start := time.Now()
	var a struct {
		ExpertID string   `json:"expertId"`
		Goal     string   `json:"goal"`
		Paths    []string `json:"paths"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", err
	}
	goal := strings.TrimSpace(a.Goal)
	if goal == "" {
		return "", fmt.Errorf("goal is required")
	}
	views, err := b.list(ctx, actor)
	if err != nil {
		return "", err
	}
	mode := "user"
	want := strings.TrimSpace(a.ExpertID)
	if want == "" {
		mode = "auto"
	}
	var chosen *ExpertView
	if want != "" {
		for i := range views {
			if views[i].ID == want {
				chosen = &views[i]
				break
			}
		}
		if chosen == nil {
			actx := authz.WithActor(ctx, actor.Authz())
			audit.Record(actx, "expert.delegate",
				"expert_id", want, "mode", mode, "outcome", "failed",
				"reason", "unknown_expert",
				"session_id", b.SessionID, "assistant_id", b.AssistantID,
				"duration_ms", time.Since(start).Milliseconds(),
			)
			return "", fmt.Errorf("unknown expert %q — call ListExperts and use a listed id", want)
		}
	} else {
		for i := range views {
			if views[i].State == ExpertReady {
				chosen = &views[i]
				break
			}
		}
		if chosen == nil {
			actx := authz.WithActor(ctx, actor.Authz())
			audit.Record(actx, "expert.delegate",
				"expert_id", "", "mode", mode, "outcome", "failed",
				"reason", "no_ready_expert",
				"session_id", b.SessionID, "assistant_id", b.AssistantID,
				"duration_ms", time.Since(start).Milliseconds(),
			)
			return "", fmt.Errorf("no ready expert — call ListExperts; configure Git / specialist login if needs_auth")
		}
	}
	if chosen.State != ExpertReady {
		actx := authz.WithActor(ctx, actor.Authz())
		audit.Record(actx, "expert.delegate",
			"expert_id", chosen.ID, "mode", mode, "outcome", chosen.State,
			"reason", chosen.Reason,
			"session_id", b.SessionID, "assistant_id", b.AssistantID,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		msg := fmt.Sprintf("expert %s is %s", chosen.ID, chosen.State)
		if chosen.Reason != "" {
			msg += " (" + chosen.Reason + ")"
		}
		if chosen.DeepLink != "" {
			msg += "; open " + chosen.DeepLink
		}
		return "", fmt.Errorf("%s", msg)
	}

	// Phase 0: accept + audit only. AttachExec coding loop is Phase 1.
	actx := authz.WithActor(ctx, actor.Authz())
	audit.Record(actx, "expert.delegate",
		"expert_id", chosen.ID, "mode", mode, "outcome", "stubbed",
		"reason", "phase0_no_attach_exec",
		"session_id", b.SessionID, "assistant_id", b.AssistantID,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	paths := a.Paths
	if paths == nil {
		paths = []string{}
	}
	resp := map[string]any{
		"status":       "stubbed",
		"outcome":      "stubbed",
		"mode":         mode,
		"expertId":     chosen.ID,
		"displayName":  chosen.DisplayName,
		"goal":         goal,
		"paths":        paths,
		"handoff":      "one_shot",
		"summary":      "Delegation accepted for validation (stub). Full AttachExec coding loop not run yet.",
		"changedPaths": []string{},
		"prIntent":     map[string]any{"action": "none"},
		"notesForUser": "Phase 0 stub: audit recorded expert.delegate outcome=stubbed. Phase 1 will run " + chosen.Command + " in /workspace.",
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
