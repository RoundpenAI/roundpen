package automode

import (
	"context"
	"strings"
)

// RequiresConfirm reports L2 high-side-effect tools that must ask the user
// under Auto even when the classifier would Allow.
func RequiresConfirm(toolName string) bool {
	switch normalizeToolName(toolName) {
	case "bash", "browser_type", "browser_click", "browser_evaluate",
		// Git delivery tools: always SoftDeny→人审 (never silent push/merge).
		"gitpush", "createpullrequest", "mergepullrequest":
		return true
	default:
		return false
	}
}

func normalizeToolName(name string) string {
	name = strings.TrimSpace(name)
	for _, sep := range []string{" · ", " — ", " - ", " "} {
		if i := strings.Index(name, sep); i > 0 {
			name = name[:i]
			break
		}
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// Resolution is the Auto-mode decision after L3 pre-check, L2 confirm list,
// and (when needed) the LLM classifier.
type Resolution struct {
	Verdict  Verdict
	AskHuman bool
	// EvalErr is set when the classifier was consulted and returned an error.
	// HardDeny / AskHuman paths may still carry a usable Verdict.
	EvalErr error
}

// Allowed reports whether Auto may run the tool without a human prompt.
func (r Resolution) Allowed() bool {
	return r.Verdict.Decision == Allow && !r.AskHuman
}

// Resolve applies deterministic L3 HardDeny, then L2 confirm, then the
// classifier. SoftDeny (including classifier failure) escalates to AskHuman;
// HardDeny never does.
func Resolve(ctx context.Context, eval Evaluator, req Request) Resolution {
	if v, ok := PreCheck(req); ok {
		return Resolution{Verdict: v, AskHuman: false}
	}
	l2 := RequiresConfirm(req.Name) || RequiresConfirm(req.Title)
	if eval == nil {
		if l2 {
			return Resolution{
				Verdict: Verdict{
					Decision: SoftDeny,
					Rule:     "high_side_effect",
					Reason:   "this tool can change the system or an authenticated browser session; confirm before running",
				},
				AskHuman: true,
			}
		}
		// No classifier configured: ask the human with a plain title (same as
		// the interactive ModeDecide path when Evaluator is nil).
		return Resolution{AskHuman: true}
	}
	v, err := eval.Evaluate(ctx, req)
	if err != nil {
		// Evaluate implementations fail closed as SoftDeny; escalate to human.
		if v.Decision == "" {
			v = failClosed(err)
		}
		if v.Decision == HardDeny {
			return Resolution{Verdict: v, EvalErr: err}
		}
		v.Decision = SoftDeny
		return Resolution{Verdict: v, AskHuman: true, EvalErr: err}
	}
	if v.Decision == HardDeny {
		return Resolution{Verdict: v}
	}
	// L2 tools never silent-Allow under Auto; SoftDeny and Allow both ask.
	if l2 {
		return Resolution{
			Verdict: Verdict{
				Decision: SoftDeny,
				Rule:     "high_side_effect",
				Reason:   "this tool can change the system or an authenticated browser session; confirm before running",
			},
			AskHuman: true,
		}
	}
	switch v.Decision {
	case Allow:
		return Resolution{Verdict: v}
	default:
		return Resolution{Verdict: v, AskHuman: true}
	}
}
