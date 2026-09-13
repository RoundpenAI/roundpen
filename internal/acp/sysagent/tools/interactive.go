package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// AnswerOption is one selectable choice in an AskUserQuestion prompt.
type AnswerOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AskQuestion is one multiple-choice question presented to the user.
type AskQuestion struct {
	Question    string         `json:"question"`
	Header      string         `json:"header"`
	Options     []AnswerOption `json:"options"`
	MultiSelect bool           `json:"multiSelect,omitempty"`
}

// Interactor is the per-session user-interaction surface interactive tools
// (AskUserQuestion, EnterPlanMode, ExitPlanMode) call into. The agent wires it
// onto the tool-call context; tools without one report "interactive session
// not available" instead of failing silently.
type Interactor struct {
	// AskUser presents one multiple-choice question and returns the label the
	// user chose. An "Other" free-text option is provided automatically.
	AskUser func(ctx context.Context, q AskQuestion) (string, error)
	// InPlanMode reports whether the session is currently in plan mode.
	InPlanMode func() bool
	// SetPlanMode toggles plan mode for the session.
	SetPlanMode func(on bool)
}

type interactorCtxKey struct{}

// WithInteractor attaches the session interactor to ctx for tool dispatches.
func WithInteractor(ctx context.Context, i *Interactor) context.Context {
	return context.WithValue(ctx, interactorCtxKey{}, i)
}

// SessionInteractor returns the interactor attached to ctx, or nil.
func SessionInteractor(ctx context.Context) *Interactor {
	i, _ := ctx.Value(interactorCtxKey{}).(*Interactor)
	return i
}

const (
	askUserQuestionHeaderMax = 30
	askUserQuestionOptsMin   = 2
	askUserQuestionOptsMax   = 4
)

// AskUserQuestionMin returns the minimum number of options a question must carry.
func AskUserQuestionMin() int { return askUserQuestionOptsMin }

// AskUserQuestionMax returns the maximum number of options a question may carry.
func AskUserQuestionMax() int { return askUserQuestionOptsMax }

// RegisterInteractive adds AskUserQuestion, EnterPlanMode and ExitPlanMode.
// These tools reach the user through the session-interaction surface that
// agent.go supplies via WithInteractor.
func RegisterInteractive(r *Registry) {
	if r == nil {
		return
	}
	r.Register(Tool{
		Name: "AskUserQuestion",
		Description: "Ask the user a multiple choice question to gather information, " +
			"clarify ambiguity, understand preferences, or make a decision. " +
			"The user is prompted interactively; an 'Other' free-text option is added automatically. " +
			"Use for the few decisions that genuinely need the user, not for routine actions.",
		Parameters: objectSchema(map[string]any{
			"question": map[string]any{"type": "string", "description": "The question to ask. Clear, specific, ending with '?'"},
			"header":   map[string]any{"type": "string", "description": "Very short label/tag for the question (max 30 chars)"},
			"options": map[string]any{
				"type":        "array",
				"minItems":    askUserQuestionOptsMin,
				"maxItems":    askUserQuestionOptsMax,
				"description": "2-4 distinct, mutually exclusive choices. No 'Other' option - it is provided automatically.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":       map[string]any{"type": "string", "description": "Display text for the choice (1-5 words)"},
						"description": map[string]any{"type": "string", "description": "What this choice means or implies"},
					},
					"required": []any{"label"},
				},
			},
		}, "question", "options"),
		Call: func(ctx context.Context, _ Actor, args json.RawMessage) (string, error) {
			var in struct {
				Question string         `json:"question"`
				Header   string         `json:"header"`
				Options  []AnswerOption `json:"options"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			in.Question = strings.TrimSpace(in.Question)
			in.Header = strings.TrimSpace(in.Header)
			if in.Question == "" {
				return "", fmt.Errorf("question is required")
			}
			if len(in.Header) > askUserQuestionHeaderMax {
				return "", fmt.Errorf("header must be at most %d chars", askUserQuestionHeaderMax)
			}
			if len(in.Options) < askUserQuestionOptsMin || len(in.Options) > askUserQuestionOptsMax {
				return "", fmt.Errorf("provide %d-%d options", askUserQuestionOptsMin, askUserQuestionOptsMax)
			}
			for i := range in.Options {
				if strings.TrimSpace(in.Options[i].Label) == "" {
					return "", fmt.Errorf("option labels cannot be empty")
				}
			}
			si := SessionInteractor(ctx)
			if si == nil || si.AskUser == nil {
				return "", fmt.Errorf("interactive session not available")
			}
			answer, err := si.AskUser(ctx, AskQuestion{
				Question: in.Question,
				Header:   in.Header,
				Options:  in.Options,
			})
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]any{
				"question": in.Question,
				"answer":   answer,
			})
			return string(out), nil
		},
	})

	r.Register(Tool{
		Name: "EnterPlanMode",
		Description: "Requests permission to enter plan mode for complex tasks that need " +
			"exploration and design before making changes. In plan mode the agent only reads " +
			"and explores; mutating tools (Bash, Write, Edit) are blocked until the plan is approved.",
		Parameters: objectSchema(map[string]any{}),
		Call: func(ctx context.Context, _ Actor, _ json.RawMessage) (string, error) {
			si := SessionInteractor(ctx)
			if si == nil || si.InPlanMode == nil || si.SetPlanMode == nil {
				return "", fmt.Errorf("interactive session not available")
			}
			if si.InPlanMode() {
				return "You are already in plan mode. Continue exploring and designing, then call ExitPlanMode when ready.", nil
			}
			si.SetPlanMode(true)
			return "Entered plan mode. Do not write or edit any files yet. " +
				"Thoroughly explore the codebase, identify existing patterns, consider approaches and trade-offs, " +
				"and use AskUserQuestion if you need to clarify the approach. " +
				"When you have designed a concrete implementation strategy, call ExitPlanMode to present it for approval.", nil
		},
	})

	r.Register(Tool{
		Name: "ExitPlanMode",
		Description: "Presents the finished plan for user approval and, once approved, " +
			"exits plan mode so implementation can begin. Only usable while in plan mode. " +
			"Do not use AskUserQuestion to ask 'should I proceed?' - that is what this tool is for.",
		Parameters: objectSchema(map[string]any{
			"plan": map[string]any{"type": "string", "description": "The plan to present for approval (optional)"},
		}),
		Call: func(ctx context.Context, _ Actor, args json.RawMessage) (string, error) {
			var in struct {
				Plan string `json:"plan"`
			}
			_ = json.Unmarshal(args, &in)
			si := SessionInteractor(ctx)
			if si == nil || si.AskUser == nil || si.InPlanMode == nil || si.SetPlanMode == nil {
				return "", fmt.Errorf("interactive session not available")
			}
			if !si.InPlanMode() {
				return "You are not in plan mode. ExitPlanMode is only for exiting plan mode after writing a plan, " +
					"and ExitPlanMode itself reads the plan from the conversation - you do not need to call it for an approved plan.", nil
			}
			question := "Approve the plan and start implementing?"
			if plan := strings.TrimSpace(in.Plan); plan != "" {
				question = "Approve this plan?\n\n" + plan
			}
			answer, err := si.AskUser(ctx, AskQuestion{
				Question: question,
				Header:   "Plan approval",
				Options: []AnswerOption{
					{Label: "Approve", Description: "Exit plan mode and start implementing the plan"},
					{Label: "Reject", Description: "Keep working on the plan; request changes first"},
				},
			})
			if err != nil {
				return "", err
			}
			approved := strings.EqualFold(strings.TrimSpace(answer), "approve")
			if !approved {
				return "The user rejected the plan. Rework the plan based on their feedback while remaining in plan mode, then call ExitPlanMode again when ready.", nil
			}
			si.SetPlanMode(false)
			return "Plan approved. You are no longer in plan mode - start implementing the approved plan now.", nil
		},
	})
}
