// Package sysagent implements the in-process System Management ACP agent.
package sysagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/policy"
	"github.com/RoundpenAI/roundpen/internal/toolgw"
)

// keepLastToolResults is how many recent tool payloads stay verbatim in-context.
const keepLastToolResults = 6

// PermissionMetaKindKey marks permission requests that present a question or
// plan approval to the user. The client treats those as dialogs and skips the
// auto-mode classifier, which only judges tool actions.
//
// PermissionMetaAskHumanKey marks SoftDeny / L2 escalations from the in-process
// agent so the runner does not re-classify and auto-allow the same call.
const (
	PermissionMetaKindKey     = "roundpen.kind"
	PermissionKindQuestion    = "question"
	PermissionMetaAskHumanKey = "roundpen.ask_human"
)

// maxSkillInvocationsPerTurn bounds Skill nesting within one reply.
const maxSkillInvocationsPerTurn = 3

// steerInboxSize bounds steered messages waiting for the next turn checkpoint.
const steerInboxSize = 8

var (
	// ErrNoActiveTurn is returned when a steer arrives with no turn running;
	// the caller falls back to queueing the message.
	ErrNoActiveTurn = errors.New("sysagent: no active turn")
	// ErrInboxFull is returned when steered messages outpace the checkpoints.
	ErrInboxFull = errors.New("sysagent: steer inbox full")
)

type session struct {
	id         string
	cancel     context.CancelFunc
	allowAll   bool
	allowTools map[string]bool
	denyTools  map[string]bool
	planMode   bool
	permMode   string

	// inbox carries mid-turn user messages (steering) from the runner; the
	// turn loop drains it at checkpoints. turnSeq lets a finishing turn clear
	// its own cancel func without clobbering a successor's.
	inbox   chan string
	turnSeq uint64
}

// Deps wires LLM + tools for one agent instance.
type Deps struct {
	LLM       LLMConfig
	Tools     *tools.Registry
	// Gateway is the unified tool invoke path. When nil, New wraps Tools.
	Gateway *toolgw.Gateway
	// Policy is the optional assistant constitution enforced inside toolgw
	// (nil = NAS zero-config: only L3 HardDeny, no soft policy friction).
	// Prefer PolicyLookup when the constitution can change mid-session
	// (capability grants, detail toggles); Policy alone is a static snapshot.
	Policy *policy.Profile
	// PolicyLookup reloads the constitution on each tool invoke when set.
	PolicyLookup func(ctx context.Context) *policy.Profile
	Actor        tools.Actor
	History     MessageSource
	SessionID   string // Roundpen agent session id (for history replay)
	AssistantID string
	Evaluator   automode.Evaluator
	// Profile selects system prompt + is mirrored in tools.SessionSurface.
	// Empty means the default System Agent persona.
	Profile string
	// Cwd is the guest absolute working directory for this session
	// (e.g. /workspace/projects/foo). Empty means /workspace.
	Cwd string
	// ProjectKey / ProjectName / RepoPath bind coordinator & workers to a Project.
	ProjectKey  string
	ProjectName string
	RepoPath    string
	// UserContext is a short "about the human" block appended to the system
	// prompt (empty = skip). Built from the user's profile settings.
	UserContext string
	// TokenBudget is a soft ceiling for this agent instance (0 = unlimited).
	// When exceeded mid-turn after a tool batch, the turn ends with a nudge to
	// FinishRun(continue=true) for routine runs.
	TokenBudget int
	// OnTokens reports each completion's total tokens (for routine persistence).
	OnTokens func(total int)
}

// Agent is an ACP agent that runs an OpenAI tool-calling loop.
type Agent struct {
	deps        Deps
	conn        *acp.AgentSideConnection
	sessions    map[string]*session
	permDefault string
	tokensUsed  int
	mu          sync.Mutex
}

var _ acp.Agent = (*Agent)(nil)

// New creates a System Agent.
func New(deps Deps) *Agent {
	if deps.Tools == nil {
		deps.Tools = tools.NewRegistry()
	}
	if deps.Gateway == nil {
		deps.Gateway = toolgw.New(deps.Tools)
	}
	return &Agent{deps: deps, sessions: make(map[string]*session)}
}

// SetAgentConnection receives the connection after construction.
func (a *Agent) SetAgentConnection(conn *acp.AgentSideConnection) { a.conn = conn }

// effectivePolicy returns the live constitution when PolicyLookup is set,
// otherwise the static Policy snapshot.
func (a *Agent) effectivePolicy(ctx context.Context) *policy.Profile {
	if a.deps.PolicyLookup != nil {
		return a.deps.PolicyLookup(ctx)
	}
	return a.deps.Policy
}

// SetTokenBudget updates the remaining token ceiling for subsequent turns.
func (a *Agent) SetTokenBudget(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deps.TokenBudget = n
}

// TokensUsed is the cumulative total reported by the upstream for this agent.
func (a *Agent) TokensUsed() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tokensUsed
}

// ResetTokenAccounting clears the instance counter and installs a new ceiling.
// Used when a continue segment rebinds the same live agent to a new run key.
func (a *Agent) ResetTokenAccounting(budget int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokensUsed = 0
	a.deps.TokenBudget = budget
}

func (a *Agent) tokenBudgetExceeded() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deps.TokenBudget <= 0 {
		return false
	}
	return a.tokensUsed >= a.deps.TokenBudget
}

func (a *Agent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: false,
			SessionCapabilities: acp.SessionCapabilities{
				Resume: &acp.SessionResumeCapabilities{},
			},
		},
	}, nil
}

func (a *Agent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	sid := randomID()
	a.mu.Lock()
	a.sessions[sid] = &session{
		id:         sid,
		allowTools: make(map[string]bool),
		denyTools:  make(map[string]bool),
		permMode:   a.permDefault,
		inbox:      make(chan string, steerInboxSize),
	}
	a.mu.Unlock()
	return acp.NewSessionResponse{SessionId: acp.SessionId(sid)}, nil
}

// SetPermMode selects how mutating tool calls are approved. Calls before
// NewSession are remembered as the default for later sessions.
func (a *Agent) SetPermMode(sid, mode string) {
	mode = automode.NormalizeMode(mode)
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[sid]; s != nil {
		s.permMode = mode
		return
	}
	a.permDefault = mode
}

// SetAutoMode keeps the old on/off switch: on is the classifier, off allows
// workspace edits and asks about every other tool.
func (a *Agent) SetAutoMode(sid string, on bool) {
	mode := automode.ModeEdit
	if on {
		mode = automode.ModeDecide
	}
	a.SetPermMode(sid, mode)
}

func (a *Agent) permMode(sid string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	mode := a.permDefault
	if s := a.sessions[sid]; s != nil {
		mode = s.permMode
	}
	if mode == "" {
		return automode.ModeDecide
	}
	return mode
}

// turnDigest renders user/assistant text for the classifier; tool results and
// reasoning are dropped so content the agent read cannot steer the verdict.
func turnDigest(messages []chatMessage) string {
	digest := make([]automode.DigestMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		digest = append(digest, automode.DigestMessage{Role: m.Role, Content: m.Content})
	}
	return automode.RecentDigest(digest, 4000)
}

func (a *Agent) Authenticate(ctx context.Context, _ acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (a *Agent) Cancel(ctx context.Context, params acp.CancelNotification) error {
	a.mu.Lock()
	s := a.sessions[string(params.SessionId)]
	a.mu.Unlock()
	if s != nil && s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (a *Agent) Prompt(_ context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	sid := string(params.SessionId)
	a.mu.Lock()
	s, ok := a.sessions[sid]
	a.mu.Unlock()
	if !ok {
		return acp.PromptResponse{}, fmt.Errorf("session %s not found", sid)
	}
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The ACP pipe cannot carry context values, so re-attach the authenticated
	// actor for the owner-scoped sandbox/memory calls made by tools.
	ctx = authz.WithActor(ctx, a.deps.Actor.Authz())
	a.mu.Lock()
	s.cancel = cancel
	s.turnSeq++
	seq := s.turnSeq
	a.mu.Unlock()
	// Clear the cancel func on every exit path, including errors. A successor
	// turn bumps turnSeq before this runs, so its cancel func survives.
	defer func() {
		a.mu.Lock()
		if s.turnSeq == seq {
			s.cancel = nil
		}
		a.mu.Unlock()
	}()

	userText := ""
	for _, block := range params.Prompt {
		if block.Text != nil {
			userText += block.Text.Text
		}
	}

	if err := a.turn(ctx, s, userText); err != nil {
		if ctx.Err() != nil {
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		}
		return acp.PromptResponse{}, err
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// SteerSession hands a mid-turn user message to the running turn. The turn
// loop drains the inbox at checkpoints (loop top, tool-batch boundary), so the
// model sees the message on its next request without cancelling work in
// flight. ErrNoActiveTurn tells the caller to queue the message instead;
// the message is already persisted, so a turn ending right now loses nothing —
// the next replay picks it up.
func (a *Agent) SteerSession(sid, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	a.mu.Lock()
	s, ok := a.sessions[sid]
	active := ok && s.cancel != nil
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %s not found", sid)
	}
	if !active {
		return ErrNoActiveTurn
	}
	select {
	case s.inbox <- text:
		return nil
	default:
		return ErrInboxFull
	}
}

func (a *Agent) turn(ctx context.Context, s *session, userText string) error {
	sid := s.id
	messages := a.buildPromptMessages(ctx, userText)
	// Steers that arrived before this turn started are already part of the
	// replay above (they were persisted); keep only ones delivered from here on.
	discardInbox(s.inbox)
	openaiTools := a.deps.Tools.OpenAITools()
	// Bound skill nesting: injected skill instructions may drive further Skill
	// calls, and an unfettered instruction chain can loop forever. Skill is
	// non-mutating, so a per-turn cap is the practical nesting guard.
	skillInvocations := 0

	var watch loopWatch
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		messages = drainInbox(s.inbox, messages)
		if historyChars(messages) > maxHistoryChars {
			messages = shrinkOldToolResults(messages, keepLastToolResults)
		}
		var streamed bool
		streamOnce := func() (chatMessage, string, error) {
			cfg := a.deps.LLM
			cfg.OnUsage = func(u TokenUsage) {
				a.mu.Lock()
				a.tokensUsed += u.Total
				a.mu.Unlock()
				if a.deps.OnTokens != nil && u.Total > 0 {
					a.deps.OnTokens(u.Total)
				}
			}
			return cfg.chatStream(ctx, messages, openaiTools,
				func(chunk string) error {
					streamed = true
					return a.emitText(ctx, sid, chunk)
				},
				func(chunk string) error {
					return a.emitThought(ctx, sid, chunk)
				},
			)
		}
		msg, finish, err := streamOnce()
		if err != nil && !streamed && ctx.Err() == nil && isTransientLLMError(err) {
			// Upstream cut the SSE stream before emitting anything visible;
			// one silent retry masks transient interruptions.
			msg, finish, err = streamOnce()
		}
		if err != nil {
			_ = a.emitText(ctx, sid, "LLM error: "+err.Error())
			return err
		}
		messages = append(messages, msg)

		if a.tokenBudgetExceeded() {
			_ = a.emitText(ctx, sid, "Token budget reached for this run. Call FinishRun with continue=true to keep going in the next segment, or FinishRun without continue if the job is done.")
			return nil
		}

		if len(msg.ToolCalls) == 0 {
			// A steer that landed while the final text streamed must not be
			// dropped: fold it in and ask the model again instead of ending.
			if steered := drainInbox(s.inbox, nil); len(steered) > 0 {
				messages = append(messages, steered...)
				continue
			}
			if streamed {
				return nil
			}
			text := msg.Content
			if text == "" {
				text = "(no response)"
			}
			return a.emitText(ctx, sid, text)
		}

		// Use = not := so we reassign the outer messages (tool results must
		// survive into the next LLM round; := would shadow and drop them).
		var recs []toolCallRec
		recs, messages, err = a.runToolBatch(ctx, sid, msg.ToolCalls, messages, &skillInvocations)
		if err != nil {
			return err
		}
		_ = finish
		nudge, stop := watch.observe(recs)
		if stop {
			return a.emitText(ctx, sid, watch.stopText())
		}
		if nudge {
			thought := "Same actions repeating — changing approach."
			if watch.lastKind == "agent" {
				thought = "Agent workspace is absent — use Bash or a file tool; do not stop after listing."
			} else if watch.infra > 0 {
				thought = "Browser Chrome is not ready — wait and retry browser_* once, do not keep clicking."
			}
			_ = a.conn.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: acp.SessionId(sid),
				Update:    acp.UpdateAgentThoughtText(thought),
			})
			messages = append(messages, chatMessage{Role: "user", Content: watch.nudgeText()})
		}
	}
}

// drainInbox folds steered user messages into the running conversation. It is
// called only where the message list is otherwise settled (loop top before an
// LLM request, just before the turn would end) so appended user rows keep
// tool_call/tool_result pairing intact.
func drainInbox(inbox <-chan string, messages []chatMessage) []chatMessage {
	for {
		select {
		case t := <-inbox:
			if strings.TrimSpace(t) != "" {
				messages = append(messages, chatMessage{Role: "user", Content: t})
			}
		default:
			return messages
		}
	}
}

// discardInbox drops leftovers from earlier turns; those rows are in the
// database, so the history replay already carries them.
func discardInbox(inbox <-chan string) {
	for {
		select {
		case <-inbox:
		default:
			return
		}
	}
}

// finishToolCall sends a terminal tool-call state. The notification context is
// detached from turn cancellation: SendNotification refuses on a cancelled ctx,
// which would otherwise leave the call spinning in the UI forever.
func (a *Agent) finishToolCall(ctx context.Context, sid, callID string, status acp.ToolCallStatus, result string) error {
	var outObj any = result
	var parsed any
	if err := json.Unmarshal([]byte(result), &parsed); err == nil {
		outObj = parsed
	}
	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return a.conn.SessionUpdate(updateCtx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update: acp.UpdateToolCall(
			acp.ToolCallId(callID),
			acp.WithUpdateStatus(status),
			acp.WithUpdateRawOutput(outObj),
			acp.WithUpdateContent([]acp.ToolCallContent{
				acp.ToolContent(acp.TextBlock(result)),
			}),
		),
	})
}

func (a *Agent) emitText(ctx context.Context, sid, text string) error {
	return a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update:    acp.UpdateAgentMessageText(text),
	})
}

// emitThought surfaces genuine model reasoning (reasoning_content deltas) as
// ACP ability_thought events. Whitespace-only deltas are dropped.
func (a *Agent) emitThought(ctx context.Context, sid, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	err := a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update:    acp.UpdateAgentThoughtText(text),
	})
	return err
}

// isTransientLLMError reports whether an LLM stream failure is worth one retry:
// context cancellation is not retried, 4xx are client errors, but 5xx and any
// mid-stream interruption (provider cut the SSE body) may succeed on retry.
func isTransientLLMError(err error) bool {
	if err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var he *httpStatusError
	if errors.As(err, &he) {
		return he.code >= 500
	}
	return true
}

// toolPermCached returns a prior session decision for a mutating tool, if any.
func (a *Agent) toolPermCached(sid, toolName string) (allowed bool, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sid]
	if s == nil {
		return false, false
	}
	if s.denyTools[toolName] {
		return false, true
	}
	if s.allowAll || s.allowTools[toolName] {
		return true, true
	}
	return false, false
}

func formatAutoReason(v automode.Verdict) string {
	reason := strings.TrimSpace(v.Rule)
	if r := strings.TrimSpace(v.Reason); r != "" {
		if reason != "" {
			reason += " — "
		}
		reason += r
	}
	return reason
}

func annotatePermissionTitle(title string, v automode.Verdict) string {
	rule := strings.TrimSpace(v.Rule)
	if rule == "" {
		return title
	}
	out := title + " · " + rule
	if r := strings.TrimSpace(v.Reason); r != "" {
		out += " — " + r
	}
	return out
}

// askToolPermission presents the standard tool permission dialog.
func (a *Agent) askToolPermission(ctx context.Context, sid, callID, toolName, title string, v automode.Verdict) (bool, error) {
	opts := []acp.PermissionOption{
		{OptionId: acp.PermissionOptionId("allow"), Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: acp.PermissionOptionId("allow_tool"), Name: "Allow this tool (session)", Kind: acp.PermissionOptionKindAllowAlways},
		{OptionId: acp.PermissionOptionId("allow_all"), Name: "Allow all tools (session)", Kind: acp.PermissionOptionKindAllowAlways},
		{OptionId: acp.PermissionOptionId("reject"), Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
		{OptionId: acp.PermissionOptionId("reject_tool"), Name: "Reject this tool (session)", Kind: acp.PermissionOptionKindRejectAlways},
	}
	meta := map[string]any{}
	if rule := strings.TrimSpace(v.Rule); rule != "" {
		meta["roundpen.auto_rule"] = rule
		meta[PermissionMetaAskHumanKey] = true
	}
	if reason := strings.TrimSpace(v.Reason); reason != "" {
		meta["roundpen.auto_reason"] = reason
		meta[PermissionMetaAskHumanKey] = true
	}
	var metaArg map[string]any
	if len(meta) > 0 {
		metaArg = meta
	}
	resp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(sid),
		Meta:      metaArg,
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId(callID),
			Title:      &title,
		},
		Options: opts,
	})
	if err != nil {
		return false, err
	}
	opt := ""
	if resp.Outcome.Selected != nil {
		opt = string(resp.Outcome.Selected.OptionId)
	}
	return a.applyToolPerm(sid, toolName, opt), nil
}

// annotateAutoApproved updates the tool-call title so the UI can show the
// classifier rule that auto-approved the call.
func (a *Agent) annotateAutoApproved(ctx context.Context, sid, callID, title string, v automode.Verdict) error {
	rule := strings.TrimSpace(v.Rule)
	if rule == "" || a.conn == nil {
		return nil
	}
	annotated := title + " · 自动审核通过 · " + rule
	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return a.conn.SessionUpdate(updateCtx, acp.SessionNotification{
		SessionId: acp.SessionId(sid),
		Update: acp.UpdateToolCall(
			acp.ToolCallId(callID),
			acp.WithUpdateTitle(annotated),
		),
	})
}

// applyToolPerm records the user's choice and returns whether the call may proceed.
func (a *Agent) applyToolPerm(sid, toolName, optionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sid]
	if s == nil {
		return optionID == "allow" || optionID == "allow_tool" || optionID == "allow_all"
	}
	switch optionID {
	case "allow":
		return true
	case "allow_tool":
		s.allowTools[toolName] = true
		delete(s.denyTools, toolName)
		return true
	case "allow_all":
		s.allowAll = true
		return true
	case "reject_tool":
		s.denyTools[toolName] = true
		delete(s.allowTools, toolName)
		return false
	default: // reject / cancelled / unknown
		return false
	}
}

// skillMutates reports whether a Skill tool call mutates the workspace
// (install/remove); invoke and list only read.
func skillMutates(args json.RawMessage) bool {
	var in struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(args, &in)
	switch strings.TrimSpace(in.Action) {
	case "", "invoke", "list":
		return false
	}
	return true
}

func (a *Agent) Logout(ctx context.Context, params acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, acp.NewMethodNotFound(acp.AgentMethodLogout)
}
func (a *Agent) CloseSession(ctx context.Context, params acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionClose)
}

// inPlanMode reports whether the session is currently in plan mode.
func (a *Agent) inPlanMode(sid string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sid]
	return s != nil && s.planMode
}

// setPlanMode toggles plan mode for the session.
func (a *Agent) setPlanMode(sid string, on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[sid]; s != nil {
		s.planMode = on
	}
}

// interactor builds the tools.Interactor surface for the given session, used to
// back AskUserQuestion / EnterPlanMode / ExitPlanMode during tool dispatch.
func (a *Agent) interactor(sid string) *tools.Interactor {
	return &tools.Interactor{
		AskUser:     func(ctx context.Context, q tools.AskQuestion) (string, error) { return a.askUser(ctx, sid, q) },
		InPlanMode:  func() bool { return a.inPlanMode(sid) },
		SetPlanMode: func(on bool) { a.setPlanMode(sid, on) },
	}
}

// askUser presents a multiple-choice question to the user through the ACP
// permission dialog (the web console renders the option buttons) and returns
// the chosen option label.
func (a *Agent) askUser(ctx context.Context, sid string, q tools.AskQuestion) (string, error) {
	if a.conn == nil {
		return "", fmt.Errorf("agent connection not ready")
	}
	if len(q.Options) < tools.AskUserQuestionMin() {
		return "", fmt.Errorf("at least 2 options are required")
	}
	// OptionIds are index-based, not label-based: labels may repeat or collide
	// with the synthetic "other" id; the chosen id maps back to its label.
	opts := make([]acp.PermissionOption, 0, len(q.Options)+1)
	labels := make([]string, 0, len(q.Options)+1)
	for _, o := range q.Options {
		if strings.TrimSpace(o.Label) == "" {
			continue
		}
		labels = append(labels, o.Label)
		opts = append(opts, acp.PermissionOption{
			OptionId: acp.PermissionOptionId(fmt.Sprintf("opt-%d", len(labels)-1)),
			Name:     o.Label,
			Kind:     acp.PermissionOptionKindAllowOnce,
		})
	}
	if len(opts) < tools.AskUserQuestionMax() {
		labels = append(labels, "")
		opts = append(opts, acp.PermissionOption{
			OptionId: acp.PermissionOptionId("other"),
			Name:     "Other …",
			Kind:     acp.PermissionOptionKindAllowOnce,
		})
	}

	title := strings.TrimSpace(q.Header) + " · " + strings.TrimSpace(q.Question)
	title = strings.Trim(title, " ·")
	if title == "" {
		title = strings.TrimSpace(q.Question)
	}
	resp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(sid),
		Meta:      map[string]any{PermissionMetaKindKey: PermissionKindQuestion},
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId("ask-" + randomID()),
			Title:      &title,
		},
		Options: opts,
	})
	if err != nil {
		return "", err
	}
	if resp.Outcome.Cancelled != nil {
		return "", fmt.Errorf("user cancelled the question")
	}
	if resp.Outcome.Selected == nil || strings.TrimSpace(string(resp.Outcome.Selected.OptionId)) == "" {
		return "", fmt.Errorf("no answer provided")
	}
	id := string(resp.Outcome.Selected.OptionId)
	if id == "other" {
		return "Other (see user reply)", nil
	}
	var idx int
	if _, err := fmt.Sscanf(id, "opt-%d", &idx); err != nil || idx < 0 || idx >= len(labels) || labels[idx] == "" {
		return "", fmt.Errorf("unknown answer option %q", id)
	}
	return labels[idx], nil
}

func (a *Agent) ListSessions(ctx context.Context, params acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
}
func (a *Agent) ResumeSession(ctx context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	sid := string(params.SessionId)
	if strings.TrimSpace(sid) == "" {
		return acp.ResumeSessionResponse{}, fmt.Errorf("sessionId is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[sid]; s != nil {
		// Already live — clear any leftover cancel from a finished turn.
		s.cancel = nil
		return acp.ResumeSessionResponse{}, nil
	}
	a.sessions[sid] = &session{
		id:         sid,
		allowTools: make(map[string]bool),
		denyTools:  make(map[string]bool),
		permMode:   a.permDefault,
		inbox:      make(chan string, steerInboxSize),
	}
	return acp.ResumeSessionResponse{}, nil
}
func (a *Agent) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetConfigOption)
}
func (a *Agent) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// LoopbackBase turns an HTTP listen addr into a loopback URL.
func LoopbackBase(httpAddr string) string {
	host := strings.TrimSpace(httpAddr)
	if host == "" {
		return "http://127.0.0.1:9527"
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/")
	}
	if strings.HasPrefix(host, ":") {
		return "http://127.0.0.1" + host
	}
	if strings.HasPrefix(host, "0.0.0.0:") {
		return "http://127.0.0.1" + host[len("0.0.0.0"):]
	}
	if strings.HasPrefix(host, "[::]:") {
		return "http://127.0.0.1" + host[len("[::]"):]
	}
	return "http://" + host
}
