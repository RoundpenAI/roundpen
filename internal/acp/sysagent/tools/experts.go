package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/expertrun"
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

// ExpertRunner runs one-shot AttachExec specialists (Claude). Nil → Claude
// delegate fails closed with a clear error (no silent stub).
type ExpertRunner interface {
	Run(ctx context.Context, req expertrun.Request) (expertrun.Result, error)
	Busy(userID string) bool
	ProbeAuth(ctx context.Context, userID string, actor authz.Actor, expertID, command string) error
}

// ExpertBinder discovers coding ACP specialists and delegates one-shot runs.
type ExpertBinder struct {
	Envs        EnvLister
	Providers   []providers.Provider // nil → providers.Default()
	SessionID   string
	AssistantID string
	Runner      ExpertRunner
	// ModelSourceOwn reports whether the user bills Claude via guest login.
	ModelSourceOwn func(ctx context.Context, userID string) bool
	// AttachEnabled lists expert ids with real AttachExec (default: claude).
	AttachEnabled map[string]bool
}

func (b *ExpertBinder) catalog() []providers.Provider {
	if b != nil && len(b.Providers) > 0 {
		return b.Providers
	}
	return providers.Default()
}

func (b *ExpertBinder) attachOK(id string) bool {
	if b != nil && b.AttachEnabled != nil {
		return b.AttachEnabled[id]
	}
	return id == "claude"
}

func (b *ExpertBinder) deepLink() string {
	if aid := strings.TrimSpace(b.AssistantID); aid != "" {
		return "/a/" + aid + "?tab=experts"
	}
	return "/settings?section=git"
}

// RegisterExperts adds ListExperts, TestExpertConnection, and DelegateToExpert.
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
				"note":    "Call ListExperts before DelegateToExpert. Delegation is one-shot. After a successful run, use GitCommit + CreatePullRequest (always open PR; user confirms).",
			})
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	})
	r.Register(Tool{
		Name: "TestExpertConnection",
		Description: "Probe a specialist (Initialize only) on the Agent Docker slot. " +
			"Use when ListExperts shows needs_auth or before the first DelegateToExpert for Claude. " +
			"Does not run a coding task. On failure, follow deepLink — never ask the user to paste tokens in chat.",
		Parameters: objectSchema(map[string]any{
			"expertId": map[string]any{
				"type":        "string",
				"description": "Specialist id from ListExperts (default claude)",
			},
		}),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			return b.testConnect(ctx, actor, raw)
		},
	})
	r.Register(Tool{
		Name: "DelegateToExpert",
		Description: "Delegate a one-shot coding task to Claude Code (claude-agent-acp) in the shared /workspace Agent Docker. " +
			"ALWAYS call ListExperts first; only delegate when state=ready. " +
			"Omit expertId to auto-route to the first ready AttachExec-capable expert (audit mode=auto). " +
			"Pass goal plus optional repoPath/paths/branch/issueURL/prURL/changeSetSummary/constraints. " +
			"Returns summary, changedPaths, prIntent. Then deliver with GitCommit → CreatePullRequest (always open PR; SoftDeny asks the user). " +
			"Do not dump secrets or the full chat transcript into goal/constraints.",
		Parameters: objectSchema(map[string]any{
			"expertId": map[string]any{
				"type":        "string",
				"description": "Specialist id from ListExperts (e.g. claude). Omit for auto-route.",
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
			"repoPath": map[string]any{
				"type":        "string",
				"description": "Optional repo directory under /workspace",
			},
			"branch": map[string]any{
				"type":        "string",
				"description": "Optional preferred branch name",
			},
			"issueURL": map[string]any{
				"type":        "string",
				"description": "Optional issue URL",
			},
			"prURL": map[string]any{
				"type":        "string",
				"description": "Optional existing PR URL",
			},
			"changeSetSummary": map[string]any{
				"type":        "string",
				"description": "Optional short prior ChangeSet summary (not a full diff dump)",
			},
			"constraints": map[string]any{
				"type":        "string",
				"description": "Optional constraints (tests to run, do-not-touch paths). No secrets.",
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
	deep := b.deepLink()
	busy := b.Runner != nil && b.Runner.Busy(actor.Username)
	out := make([]ExpertView, 0)
	for _, p := range providers.CodingExperts(b.catalog()) {
		state, reason := b.classify(ctx, actor, p, envs, busy)
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

func (b *ExpertBinder) classify(ctx context.Context, actor Actor, p providers.Provider, envs []userenv.EnvView, busy bool) (state, reason string) {
	if !p.Enabled {
		return ExpertUnavailable, "disabled"
	}
	if !b.attachOK(p.ID) {
		return ExpertUnavailable, "attach_exec_not_enabled"
	}
	agentStatus := "absent"
	for _, e := range envs {
		if e.Slot == userenv.SlotAgent {
			agentStatus = e.Status
			break
		}
	}
	if agentStatus == "missing" {
		return ExpertUnavailable, "agent_slot_missing"
	}
	if busy {
		return ExpertBusy, "expert_running"
	}
	own := b.ModelSourceOwn != nil && b.ModelSourceOwn(ctx, actor.Username)
	if own && b.Runner != nil {
		// Soft probe is expensive; mark needs_auth only when a prior probe failed
		// is not cached — for List we stay ready and let TestExpertConnection /
		// Delegate surface needs_auth. Optional cheap signal: none for now.
		_ = own
	}
	return ExpertReady, ""
}

func (b *ExpertBinder) testConnect(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
	start := time.Now()
	var a struct {
		ExpertID string `json:"expertId"`
	}
	_ = json.Unmarshal(raw, &a)
	id := strings.TrimSpace(a.ExpertID)
	if id == "" {
		id = "claude"
	}
	actx := authz.WithActor(ctx, actor.Authz())
	if b.Runner == nil {
		audit.Record(actx, "expert.probe", "expert_id", id, "outcome", "failed", "reason", "runner_unset",
			"session_id", b.SessionID, "duration_ms", time.Since(start).Milliseconds())
		return "", fmt.Errorf("expert runner not configured")
	}
	p, ok := providers.ByID(b.catalog(), id)
	if !ok || !b.attachOK(id) {
		audit.Record(actx, "expert.probe", "expert_id", id, "outcome", "failed", "reason", "unavailable",
			"session_id", b.SessionID, "duration_ms", time.Since(start).Milliseconds())
		return "", fmt.Errorf("expert %q not available for AttachExec", id)
	}
	err := b.Runner.ProbeAuth(ctx, actor.Username, actor.Authz(), id, p.Command)
	if err != nil {
		outcome := "failed"
		reason := err.Error()
		if errors.Is(err, expertrun.ErrNeedsAuth) || looksAuth(err) {
			outcome = ExpertNeedsAuth
			reason = "vendor_login_required"
		}
		audit.Record(actx, "expert.probe", "expert_id", id, "outcome", outcome, "reason", reason,
			"session_id", b.SessionID, "assistant_id", b.AssistantID,
			"duration_ms", time.Since(start).Milliseconds())
		body, _ := json.Marshal(map[string]any{
			"status":       outcome,
			"expertId":     id,
			"deepLink":     b.deepLink(),
			"notesForUser": "Complete Claude login / Git in Settings or the Experts tab. Do not paste tokens in chat.",
			"error":        err.Error(),
		})
		// SoftDeny-style: return structured result (not opaque error) so the
		// model can show the deepLink; still non-ready for Delegate.
		return string(body), nil
	}
	audit.Record(actx, "expert.probe", "expert_id", id, "outcome", "ready",
		"session_id", b.SessionID, "assistant_id", b.AssistantID,
		"duration_ms", time.Since(start).Milliseconds())
	body, _ := json.Marshal(map[string]any{
		"status":   "ready",
		"expertId": id,
		"summary":  "Initialize succeeded; specialist is launchable on the Agent slot.",
	})
	return string(body), nil
}

func (b *ExpertBinder) delegate(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
	start := time.Now()
	var a struct {
		ExpertID         string   `json:"expertId"`
		Goal             string   `json:"goal"`
		Paths            []string `json:"paths"`
		RepoPath         string   `json:"repoPath"`
		Branch           string   `json:"branch"`
		IssueURL         string   `json:"issueURL"`
		PRURL            string   `json:"prURL"`
		ChangeSetSummary string   `json:"changeSetSummary"`
		Constraints      string   `json:"constraints"`
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
	actx := authz.WithActor(ctx, actor.Authz())
	var chosen *ExpertView
	if want != "" {
		for i := range views {
			if views[i].ID == want {
				chosen = &views[i]
				break
			}
		}
		if chosen == nil {
			audit.Record(actx, "expert.delegate",
				"expert_id", want, "mode", mode, "outcome", "failed", "reason", "unknown_expert",
				"session_id", b.SessionID, "assistant_id", b.AssistantID,
				"duration_ms", time.Since(start).Milliseconds(),
			)
			return "", fmt.Errorf("unknown expert %q — call ListExperts and use a listed id", want)
		}
	} else {
		for i := range views {
			if views[i].State == ExpertReady && b.attachOK(views[i].ID) {
				chosen = &views[i]
				break
			}
		}
		if chosen == nil {
			audit.Record(actx, "expert.delegate",
				"expert_id", "", "mode", mode, "outcome", "failed", "reason", "no_ready_expert",
				"session_id", b.SessionID, "assistant_id", b.AssistantID,
				"duration_ms", time.Since(start).Milliseconds(),
			)
			return "", fmt.Errorf("no ready expert — call ListExperts; run TestExpertConnection if needs_auth")
		}
	}
	if chosen.State == ExpertNeedsAuth || chosen.State == ExpertBusy || chosen.State == ExpertUnavailable {
		audit.Record(actx, "expert.delegate",
			"expert_id", chosen.ID, "mode", mode, "outcome", chosen.State, "reason", chosen.Reason,
			"session_id", b.SessionID, "assistant_id", b.AssistantID,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		body, _ := json.Marshal(map[string]any{
			"status":       chosen.State,
			"outcome":      chosen.State,
			"mode":         mode,
			"expertId":     chosen.ID,
			"reason":       chosen.Reason,
			"deepLink":     chosen.DeepLink,
			"notesForUser": "Specialist not ready. Follow deepLink; do not paste tokens in chat.",
		})
		return string(body), nil
	}
	if !b.attachOK(chosen.ID) {
		audit.Record(actx, "expert.delegate",
			"expert_id", chosen.ID, "mode", mode, "outcome", "unavailable", "reason", "attach_exec_not_enabled",
			"session_id", b.SessionID, "assistant_id", b.AssistantID,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return "", fmt.Errorf("expert %q AttachExec not enabled yet — use claude", chosen.ID)
	}
	if b.Runner == nil {
		audit.Record(actx, "expert.delegate",
			"expert_id", chosen.ID, "mode", mode, "outcome", "failed", "reason", "runner_unset",
			"session_id", b.SessionID, "assistant_id", b.AssistantID,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return "", fmt.Errorf("expert runner not configured")
	}

	audit.Record(actx, "expert.delegate",
		"expert_id", chosen.ID, "mode", mode, "outcome", "started",
		"session_id", b.SessionID, "assistant_id", b.AssistantID,
	)

	p, _ := providers.ByID(b.catalog(), chosen.ID)
	res, runErr := b.Runner.Run(ctx, expertrun.Request{
		UserID:           actor.Username,
		Actor:            actor.Authz(),
		ExpertID:         chosen.ID,
		Command:          firstNonEmpty(chosen.Command, p.Command),
		Args:             p.Args,
		Goal:             goal,
		Paths:            a.Paths,
		RepoPath:         a.RepoPath,
		Branch:           a.Branch,
		IssueURL:         a.IssueURL,
		PRURL:            a.PRURL,
		ChangeSetSummary: a.ChangeSetSummary,
		Constraints:      a.Constraints,
		Cwd:              "/workspace",
	})
	outcome := res.Status
	if outcome == "" {
		outcome = "failed"
	}
	reason := ""
	if runErr != nil {
		reason = runErr.Error()
		if errors.Is(runErr, expertrun.ErrNeedsAuth) || res.Status == expertrun.StatusNeedsAuth {
			outcome = ExpertNeedsAuth
			reason = "vendor_login_required"
		}
	}
	audit.Record(actx, "expert.delegate",
		"expert_id", chosen.ID, "mode", mode, "outcome", outcome, "reason", truncate(reason, 200),
		"session_id", b.SessionID, "assistant_id", b.AssistantID,
		"retried", res.Retried,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	paths := res.ChangedPaths
	if paths == nil {
		paths = []string{}
	}
	prIntent := res.PRIntent
	if prIntent == nil {
		prIntent = map[string]any{"action": "none"}
	}
	resp := map[string]any{
		"status":       firstNonEmpty(res.Status, outcome),
		"outcome":      outcome,
		"mode":         mode,
		"expertId":     chosen.ID,
		"displayName":  chosen.DisplayName,
		"goal":         goal,
		"paths":        a.Paths,
		"handoff":      "one_shot",
		"summary":      res.Summary,
		"changedPaths": paths,
		"diffStat":     res.DiffStat,
		"prIntent":     prIntent,
		"stopReason":   res.StopReason,
		"retried":      res.Retried,
		"deepLink":     b.deepLink(),
		"notesForUser": res.NotesForUser,
		"deliveryHint": "If code changed: GitCommit, then CreatePullRequest (always open PR; user confirms SoftDeny). Do not push to the default branch.",
	}
	if outcome == ExpertNeedsAuth {
		resp["notesForUser"] = "Specialist needs login. Open " + b.deepLink() + " — never paste tokens in chat."
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	// needs_auth / failed still return JSON so the orchestrator can recover;
	// only unknown/misconfig use hard errors above.
	return string(out), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func looksAuth(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, n := range []string{"needs_auth", "not logged", "login", "unauthorized", "auth"} {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
