// Package imconnect bridges IM platform connectors (cc-connect platform
// layer: Telegram, Slack, ...) to Roundpen agent sessions, so a coding agent
// can be driven from a chat app.
//
// The bridge implements cc-connect's core.Agent / core.AgentSession on top of
// Roundpen's ACP manager: every cc-connect agent session maps to one
// agentsession row plus a manager.Runtime. Inbound chat messages become
// Manager.Prompt turns; agent output is converted to core.Events and streamed
// back through the cc-connect engine; ACP permission requests surface as
// interactive allow/deny buttons in the chat.
package imconnect

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/chenhg5/cc-connect/core"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
)

// permDecisionTimeout bounds how long a permission request waits for the
// user's button press before the turn is cancelled.
const permDecisionTimeout = 10 * time.Minute

// Agent adapts Roundpen to cc-connect's core.Agent. It owns one child
// agentSession per cc-connect agent session key; sessions outlive individual
// turns and are reused until the engine closes them.
type Agent struct {
	Store       *agentsession.Store
	ACP         *manager.Manager
	Provisioner *agentenv.Provisioner
	LLMGW       *llmgw.Gateway
	ProviderID  string // default "sysadmin"
	UserID      string // Roundpen user owning the created agent sessions
	AssistantID string // stamps Create / ListByAssistant
	Role        string
	APIKey      string

	mu       sync.Mutex
	sessions map[string]*agentSession
}

// compile-time interface checks.
var (
	_ core.Agent        = (*Agent)(nil)
	_ core.AgentSession = (*agentSession)(nil)
)

// Name identifies the agent to the cc-connect engine.
func (a *Agent) Name() string { return "roundpen" }

// StartSession resumes the agent session with the given id, or creates a new
// one when id is empty. The id is the Roundpen agent-session ID, so resuming
// after a restart reattaches to the same persisted transcript.
func (a *Agent) StartSession(ctx context.Context, sessionID string) (core.AgentSession, error) {
	if a.Store == nil || a.ACP == nil {
		return nil, fmt.Errorf("imconnect: store and acp manager are required")
	}
	if sessionID != "" {
		a.mu.Lock()
		if s, ok := a.sessions[sessionID]; ok && s.Alive() {
			a.mu.Unlock()
			return s, nil
		}
		a.mu.Unlock()
	}

	providerID := a.ProviderID
	if providerID == "" {
		providerID = "sysadmin"
	}
	userID := a.userID()

	provMeta, ok := providers.ByID(a.ACP.Providers(), providerID)
	if !ok || !provMeta.Enabled {
		return nil, fmt.Errorf("imconnect: unknown provider %q", providerID)
	}

	title := "IM 会话"
	var sess *agentsession.Session
	var err error
	if sessionID != "" {
		sess, err = a.Store.Get(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("imconnect: resume session: %w", err)
		}
	} else {
		sess, err = a.Store.Create(ctx, userID, title, providerID, "", a.AssistantID)
		if err != nil {
			return nil, fmt.Errorf("imconnect: create session: %w", err)
		}
	}

	sandboxID := sess.SandboxID
	if providers.NeedsSandbox(provMeta) && sandboxID == "" {
		if a.Provisioner == nil {
			return nil, fmt.Errorf("imconnect: provider %q needs a sandbox but provisioner is unset", providerID)
		}
		prov := *a.Provisioner
		prov.Config.APIKey = a.APIKey
		prov.Config.VirtualKey = a.pickVirtualKey(ctx)
		if provMeta.TemplateID != "" {
			prov.Config.TemplateID = provMeta.TemplateID
		}
		if a.LLMGW != nil {
			prov.Config.DefaultModel = a.LLMGW.DefaultModel()
		}
		res, err := prov.Provision(ctx, sess.ID, providerID, userID)
		if err != nil {
			if sessionID == "" {
				_ = a.Store.Delete(ctx, sess.ID)
			}
			return nil, fmt.Errorf("imconnect: provision sandbox: %w", err)
		}
		_ = a.Store.UpdateSandbox(ctx, sess.ID, res.Sandbox.ID)
		sess.SandboxID = res.Sandbox.ID
		sandboxID = res.Sandbox.ID
	}

	s := &agentSession{
		agent:  a,
		sess:   sess,
		events: make(chan core.Event, 256),
		perms:  make(map[string]chan core.PermissionResult),
	}
	rt, err := a.ACP.Start(ctx, sess.ID, sandboxID, providerID, manager.StartOpts{
		Actor: manager.Actor{
			Username: userID,
			Role:     a.Role,
			APIKey:   a.APIKey,
		},
		// Permissions surface as chat buttons; never auto-approve.
		AutoApprove: false,
	})
	if err != nil {
		if existing, ok := a.ACP.Get(sess.ID); ok {
			rt = existing
		} else {
			close(s.events)
			return nil, fmt.Errorf("imconnect: start agent: %w", err)
		}
	}
	s.rt = rt
	s.ctx, s.cancel = context.WithCancel(context.Background())
	rt.SetEventHandler(s.onEvent)
	rt.SetPermissionHandler(s.onPermission)

	a.mu.Lock()
	if a.sessions == nil {
		a.sessions = make(map[string]*agentSession)
	}
	a.sessions[sess.ID] = s
	a.mu.Unlock()
	return s, nil
}

func (a *Agent) pickVirtualKey(ctx context.Context) string {
	if a.LLMGW == nil {
		return ""
	}
	keys, err := a.LLMGW.Store().ListVirtualKeys(ctx)
	if err != nil {
		return ""
	}
	for _, k := range keys {
		if k.Key == llmgw.InternalVirtualKey || !k.Enabled {
			continue
		}
		return k.Key
	}
	return ""
}

// ListSessions lists Roundpen agent sessions for this assistant (or user).
func (a *Agent) ListSessions(ctx context.Context) ([]core.AgentSessionInfo, error) {
	if a.Store == nil {
		return nil, nil
	}
	var rows []*agentsession.Session
	var err error
	if a.AssistantID != "" {
		rows, err = a.Store.ListByAssistant(ctx, a.userID(), a.AssistantID, 50)
	} else {
		rows, err = a.Store.ListByUser(ctx, a.userID(), 50)
	}
	if err != nil {
		return nil, err
	}
	out := make([]core.AgentSessionInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, core.AgentSessionInfo{ID: r.ID, Summary: r.Title, ModifiedAt: r.UpdatedAt})
	}
	return out, nil
}

func (a *Agent) userID() string {
	if a.UserID != "" {
		return a.UserID
	}
	return "admin"
}

// Stop tears down every live agent session.
func (a *Agent) Stop() error {
	a.mu.Lock()
	sessions := make([]*agentSession, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.sessions = make(map[string]*agentSession)
	a.mu.Unlock()
	for _, s := range sessions {
		_ = s.Close()
	}
	return nil
}

// agentSession is one live cc-connect agent session backed by a manager.Runtime.
type agentSession struct {
	agent *Agent
	sess  *agentsession.Session
	rt    *manager.Runtime

	ctx    context.Context
	cancel context.CancelFunc

	events chan core.Event

	mu    sync.Mutex
	perms map[string]chan core.PermissionResult // requestID → decision sink
	// reply is the accumulator for the current turn's assistant text, set
	// between runTurn start and finish; nil while idle.
	reply *strings.Builder
	dead  bool
}

// Send runs one agent turn. Manager.Prompt blocks until the turn completes, so
// the prompt runs in its own goroutine while the engine drains the event
// channel.
func (s *agentSession) Send(prompt string, _ string, _ []core.ImageAttachment, _ []core.FileAttachment) error {
	if !s.Alive() {
		return fmt.Errorf("imconnect: session is closed")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	bg := context.Background()
	_, _ = s.agent.Store.AddMessage(bg, s.sess.ID, agentsession.RoleUser, prompt, map[string]string{"type": "user", "via": "im"})
	go s.runTurn(prompt)
	return nil
}

func (s *agentSession) runTurn(prompt string) {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Minute)
	defer cancel()

	var reply strings.Builder
	s.mu.Lock()
	s.reply = &reply
	s.mu.Unlock()

	stop, err := s.agent.ACP.Prompt(ctx, s.sess.ID, prompt)

	s.mu.Lock()
	s.reply = nil
	text := strings.TrimSpace(reply.String())
	s.mu.Unlock()

	if err != nil {
		s.emit(core.Event{Type: core.EventError, SessionID: s.sess.ID, Content: err.Error(), Error: err, Done: true})
		return
	}
	if text == "" {
		text = "(no response)"
	}
	_, _ = s.agent.Store.AddMessage(context.Background(), s.sess.ID, agentsession.RoleAssistant, text,
		map[string]string{"type": "assistant", "stopReason": string(stop)})
	s.emit(core.Event{Type: core.EventResult, SessionID: s.sess.ID, Content: text, Done: true})
}

// onEvent converts ACP bridge events to cc-connect core.Events.
func (s *agentSession) onEvent(ev acpclient.Event) {
	out := core.Event{SessionID: s.sess.ID}
	switch ev.Type {
	case "agent_message":
		out.Type = core.EventText
		out.Content = ev.Text
		s.mu.Lock()
		if s.reply != nil {
			s.reply.WriteString(ev.Text)
		}
		s.mu.Unlock()
	case "agent_thought":
		out.Type = core.EventThinking
		out.Content = ev.Text
	case "tool_call", "tool_call_update":
		if ev.Type == "tool_call" {
			out.Type = core.EventToolUse
		} else {
			out.Type = core.EventToolResult
		}
		out.ToolName = ev.Title
		if in, ok := ev.Input.(string); ok {
			out.ToolInput = in
		}
		switch o := ev.Output.(type) {
		case string:
			out.ToolResult = o
		}
		out.ToolStatus = ev.Status
	case "permission":
		return // handled through the permission channel, not the event stream
	case "plan":
		out.Type = core.EventText
		out.Content = ev.Text
	default:
		return
	}
	s.emit(out)
}

// onPermission surfaces an ACP permission request as a chat button prompt and
// blocks until the user answers (or the timeout cancels the turn).
func (s *agentSession) onPermission(req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	reqID := string(req.ToolCall.ToolCallId)
	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}
	decision := make(chan core.PermissionResult, 1)
	s.mu.Lock()
	s.perms[reqID] = decision
	s.mu.Unlock()

	s.emit(core.Event{
		Type:         core.EventPermissionRequest,
		SessionID:    s.sess.ID,
		RequestID:    reqID,
		ToolName:     title,
		ToolInput:    title,
		ToolInputRaw: optionSummary(req.Options),
	})

	select {
	case res := <-decision:
		return permissionResponse(res, req.Options), nil
	case <-time.After(permDecisionTimeout):
		s.mu.Lock()
		delete(s.perms, reqID)
		s.mu.Unlock()
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
		}, nil
	case <-s.ctx.Done():
		s.mu.Lock()
		delete(s.perms, reqID)
		s.mu.Unlock()
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
		}, nil
	}
}

// RespondPermission delivers the user's button choice to the waiting handler.
func (s *agentSession) RespondPermission(requestID string, result core.PermissionResult) error {
	s.mu.Lock()
	ch := s.perms[requestID]
	delete(s.perms, requestID)
	s.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("imconnect: no pending permission %q", requestID)
	}
	select {
	case ch <- result:
	default:
	}
	return nil
}

// permissionResponse maps a cc-connect permission decision onto the ACP
// option list the agent offered.
func permissionResponse(res core.PermissionResult, opts []acp.PermissionOption) acp.RequestPermissionResponse {
	if res.Behavior == "allow" {
		if id := acpclient.PickOrdinaryAllow(opts); id != "" {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId(id)},
				},
			}
		}
	}
	// deny / unmatched allow → reject, preferring an explicit reject-once option.
	for _, o := range opts {
		if o.Kind == acp.PermissionOptionKindRejectOnce {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: o.OptionId},
				},
			}
		}
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
	}
}

func optionSummary(opts []acp.PermissionOption) map[string]any {
	out := make(map[string]any, len(opts))
	for _, o := range opts {
		out[string(o.OptionId)] = o.Name
	}
	return out
}

func (s *agentSession) emit(ev core.Event) {
	if ev.Done {
		// Terminal events (EventResult, EventError) must reach the engine —
		// dropping them leaves processInteractiveEvents blocked forever.
		select {
		case s.events <- ev:
		case <-time.After(30 * time.Second):
		}
		return
	}
	select {
	case s.events <- ev:
	default:
		// The engine is the only consumer; a full buffer means it stopped
		// draining — drop intermediate events rather than block the ACP bridge.
	}
}

// Events streams agent output to the cc-connect engine.
func (s *agentSession) Events() <-chan core.Event { return s.events }

// CurrentSessionID returns the Roundpen agent-session ID.
func (s *agentSession) CurrentSessionID() string { return s.sess.ID }

// Alive reports whether the session can still run turns.
func (s *agentSession) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.dead
}

// Close tears the session down and removes it from the agent's registry.
func (s *agentSession) Close() error {
	s.mu.Lock()
	if s.dead {
		s.mu.Unlock()
		return nil
	}
	s.dead = true
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}
	s.agent.ACP.Stop(s.sess.ID)

	s.agent.mu.Lock()
	delete(s.agent.sessions, s.sess.ID)
	s.agent.mu.Unlock()

	close(s.events)
	return nil
}
