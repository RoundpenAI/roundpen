// Package toolgw is the in-process tool gateway: a single Invoke entry for
// System Agent tool calls. It re-applies deterministic HardDeny, optionally
// enforces assistant policy, audits the call, then dispatches to the builtin
// registry. Auto / SoftDeny→人审 stay in automode — this package does not
// present a second approval UX.
package toolgw

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/audit"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/policy"
)

// Decision is the gateway outcome for one Invoke.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// Call is one tool invocation after automode (or human) has already allowed it.
type Call struct {
	Actor         tools.Actor
	SessionID     string
	AssistantID   string
	Name          string
	Args          json.RawMessage
	AutoVerdict   automode.Verdict // informational; HardDeny here still blocks
	Policy        *policy.Profile  // nil = skip soft policy (NAS zero-config)
	SkipPolicy    bool             // force-skip capability/network/dir checks
}

// Result is the gateway response returned to the agent loop.
type Result struct {
	Content  string
	Err      error
	Decision Decision
	Rule     string
	Reason   string
}

// Gateway dispatches builtin tools after hard gates.
type Gateway struct {
	Builtin *tools.Registry
	Log     *slog.Logger
}

// New wraps a builtin registry. reg must be non-nil for successful Invokes.
func New(reg *tools.Registry) *Gateway {
	return &Gateway{Builtin: reg, Log: slog.Default()}
}

// OpenAITools exposes the catalog for the LLM (same as Registry).
func (g *Gateway) OpenAITools() []map[string]any {
	if g == nil || g.Builtin == nil {
		return nil
	}
	return g.Builtin.OpenAITools()
}

// Get reports whether a builtin tool exists.
func (g *Gateway) Get(name string) (tools.Tool, bool) {
	if g == nil || g.Builtin == nil {
		return tools.Tool{}, false
	}
	return g.Builtin.Get(name)
}

// Invoke runs hard denies + optional policy, then the builtin tool.
// On deny, Content carries a structured message for the model (no new dialog).
func (g *Gateway) Invoke(ctx context.Context, c Call) Result {
	start := time.Now()
	name := strings.TrimSpace(c.Name)
	args := c.Args
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}

	res := Result{Decision: DecisionAllow}
	defer func() {
		g.audit(ctx, c, res, time.Since(start))
	}()

	if g == nil || g.Builtin == nil {
		res.Decision = DecisionDeny
		res.Rule = "gateway_misconfigured"
		res.Reason = "tool gateway has no builtin registry"
		res.Content = denyMessage(res)
		res.Err = fmt.Errorf("%s", res.Reason)
		return res
	}

	// L3 re-check (deterministic). Even human Allow cannot pass HardDeny.
	if v, ok := automode.PreCheck(automode.Request{
		Name:  name,
		Title: name,
		Args:  string(args),
	}); ok && v.Decision == automode.HardDeny {
		res.Decision = DecisionDeny
		res.Rule = v.Rule
		res.Reason = v.Reason
		res.Content = denyMessage(res)
		res.Err = fmt.Errorf("blocked by toolgw: %s", formatRule(v.Rule, v.Reason))
		return res
	}
	if c.AutoVerdict.Decision == automode.HardDeny {
		res.Decision = DecisionDeny
		res.Rule = nonEmpty(c.AutoVerdict.Rule, "auto_hard_deny")
		res.Reason = c.AutoVerdict.Reason
		res.Content = denyMessage(res)
		res.Err = fmt.Errorf("blocked by toolgw: %s", formatRule(res.Rule, res.Reason))
		return res
	}

	if !c.SkipPolicy {
		if deny, rule, reason := checkPolicy(c.Policy, name, args); deny {
			res.Decision = DecisionDeny
			res.Rule = rule
			res.Reason = reason
			res.Content = denyMessage(res)
			res.Err = fmt.Errorf("blocked by policy: %s", formatRule(rule, reason))
			return res
		}
	}

	out, err := g.Builtin.Call(ctx, c.Actor, name, args)
	res.Content = out
	res.Err = err
	if err != nil && strings.TrimSpace(res.Content) == "" {
		res.Content = tools.FormatToolError(err)
	}
	return res
}

func (g *Gateway) audit(ctx context.Context, c Call, res Result, d time.Duration) {
	log := g.Log
	if log == nil {
		log = slog.Default()
	}
	ctx = authz.WithActor(ctx, c.Actor.Authz())
	attrs := []any{
		"session", c.SessionID,
		"assistant", c.AssistantID,
		"tool", c.Name,
		"decision", string(res.Decision),
		"ms", d.Milliseconds(),
	}
	if res.Rule != "" {
		attrs = append(attrs, "rule", res.Rule)
	}
	if res.Reason != "" {
		attrs = append(attrs, "reason", truncate(res.Reason, 200))
	}
	if res.Err != nil && res.Decision == DecisionAllow {
		attrs = append(attrs, "err", res.Err.Error())
	}
	audit.Record(ctx, "toolgw.invoke", attrs...)
	_ = log // audit already slog; keep Log for future sinks
}

func denyMessage(res Result) string {
	return fmt.Sprintf("Blocked by tool gateway: %s. Do not retry or route around this; choose a safer approach.",
		formatRule(res.Rule, res.Reason))
}

func formatRule(rule, reason string) string {
	rule = strings.TrimSpace(rule)
	reason = strings.TrimSpace(reason)
	switch {
	case rule != "" && reason != "":
		return rule + " — " + reason
	case rule != "":
		return rule
	default:
		return reason
	}
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
