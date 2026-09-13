package agentapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

const turnTimeout = 10 * time.Minute

// permWait tracks an outstanding interactive permission request that may outlive
// any single WebSocket connection.
type permWait struct {
	ch       chan string
	title    string
	options  any
	ticketID string
}

// runner owns the ACP turn lifecycle for one agent session: prompt execution,
// reply/thought buffering, permission handling, message persistence, and event
// fan-out to all connected WebSocket clients.  Closing a tab or switching tabs
// no longer interrupts generation; connections simply attach/detach.
type runner struct {
	handler *Handler
	session *agentsession.Session
	acp     *manager.Manager
	rt      *manager.Runtime

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	clients map[*wsClient]struct{}
	busy    bool
	auto    bool
	pending []string // queued prompts while a turn is in progress

	reply   strings.Builder
	thought strings.Builder
	thinkAt time.Time

	perms map[string]*permWait
}

// subscribe attaches a WebSocket client to the runner's broadcast.
func (r *runner) subscribe(c *wsClient) {
	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.mu.Unlock()
}

// unsubscribe removes a WebSocket client from the session and marks it closed.
func (r *runner) unsubscribe(c *wsClient) {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	r.mu.Lock()
	delete(r.clients, c)
	r.mu.Unlock()
}

func (r *runner) broadcast(v any) {
	r.mu.Lock()
	clients := make([]*wsClient, 0, len(r.clients))
	for c := range r.clients {
		clients = append(clients, c)
	}
	r.mu.Unlock()
	for _, c := range clients {
		c.write(v)
	}
}

// snapshot returns the current runner state so a freshly-connected client can
// rebuild its UI without losing the in-progress turn.
type statusSnapshot struct {
	Type    string        `json:"type"` // "status"
	Busy    bool          `json:"busy"`
	Reply   string        `json:"reply,omitempty"`
	Thought string        `json:"thought,omitempty"`
	Perm    *permSnapshot `json:"perm,omitempty"`
}

type permSnapshot struct {
	RequestID string `json:"requestId"`
	Title     string `json:"title"`
	Options   any    `json:"options"`
	TicketID  string `json:"ticketId,omitempty"`
}

func (r *runner) snapshot() statusSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := statusSnapshot{
		Type:    "status",
		Busy:    r.busy,
		Reply:   r.reply.String(),
		Thought: r.thought.String(),
	}
	for reqID, pw := range r.perms {
		s.Perm = &permSnapshot{
			RequestID: reqID,
			Title:     pw.title,
			Options:   pw.options,
			TicketID:  pw.ticketID,
		}
		break
	}
	return s
}

// persist saves a row in the session transcript.
func (r *runner) persist(role, content string, meta any) {
	_, _ = r.handler.Store.AddMessage(r.ctx, r.session.ID, role, content, meta)
}

// flushThought persists any buffered reasoning text as a thought row and resets
// the buffer.  This is called at transitions (message / tool / plan / permission
// boundaries) so that the client sees each discrete thought block.
func (r *runner) flushThought() {
	r.mu.Lock()
	text := strings.TrimSpace(r.thought.String())
	r.thought.Reset()
	var dur int64
	if !r.thinkAt.IsZero() {
		dur = time.Since(r.thinkAt).Milliseconds()
		r.thinkAt = time.Time{}
	}
	r.mu.Unlock()
	if text == "" {
		return
	}
	r.persist(agentsession.RoleThought, text, map[string]any{
		"type":       "thought",
		"durationMs": dur,
	})
}

// flushBefore resets the thought buffer for the given event type.
func (r *runner) flushBefore(typ string) {
	switch typ {
	case "agent_message", "tool_call", "tool_call_update", "plan", "permission":
		r.flushThought()
	}
}

// onEvent is wired to the ACP bridge exactly once per runtime.
func (r *runner) onEvent(ev acpclient.Event) {
	r.broadcast(wsOut{Type: "event", Event: ev})
	r.flushBefore(ev.Type)
	switch ev.Type {
	case "agent_message":
		if ev.Text == "" {
			return
		}
		r.mu.Lock()
		r.reply.WriteString(ev.Text)
		r.mu.Unlock()
	case "agent_thought":
		if ev.Text == "" {
			return
		}
		r.mu.Lock()
		if r.thought.Len() == 0 {
			r.thinkAt = time.Now()
		}
		r.thought.WriteString(ev.Text)
		r.mu.Unlock()
	case "tool_call", "tool_call_update":
		_, _ = r.handler.Store.UpsertToolMessage(r.ctx, r.session.ID, agentsession.ToolMeta{
			Type:   "tool_call",
			ToolID: ev.ToolID,
			Title:  ev.Title,
			Status: ev.Status,
			Kind:   ev.Kind,
			Input:  ev.Input,
			Output: ev.Output,
		})
	case "plan":
		text := strings.TrimSpace(ev.Text)
		if text == "" {
			text = "plan"
		}
		r.persist(agentsession.RoleEvent, text, map[string]string{"type": "plan"})
	case "permission":
		title := strings.TrimSpace(ev.Title)
		if title == "" {
			title = "tool"
		}
		outcome := ev.Status
		if outcome == "" {
			outcome = "auto"
		}
		r.persist(agentsession.RolePermission, title+" · "+ev.Text, agentsession.PermissionMeta{
			Type:     "permission",
			Title:    title,
			OptionID: ev.Text,
			Outcome:  outcome,
			ToolID:   ev.ToolID,
		})
	}
}

// prompt queues a new user message and starts a turn if the runner is idle.
func (r *runner) prompt(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	r.persist(agentsession.RoleUser, text, map[string]string{"type": "user"})
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy {
		r.pending = append(r.pending, text)
		return
	}
	r.beginTurnLocked(text)
}

func (r *runner) beginTurnLocked(text string) {
	r.busy = true
	r.reply.Reset()
	r.thought.Reset()
	r.thinkAt = time.Time{}
	go r.runTurn(text)
}

func (r *runner) runTurn(text string) {
	// Tell connected clients a turn started (queued turn, other tabs) so the
	// composer locks immediately, not on the first streamed event.
	r.broadcast(r.snapshot())
	ctx, cancel := context.WithTimeout(r.ctx, turnTimeout)
	defer cancel()
	stop, err := r.acp.Prompt(ctx, r.session.ID, text)
	if err != nil {
		r.flushThought()
		r.persist(agentsession.RoleEvent, err.Error(), map[string]string{"type": "error"})
		r.broadcast(wsOut{Type: "error", Message: err.Error()})
	} else {
		r.flushThought()
		r.mu.Lock()
		assistant := strings.TrimSpace(r.reply.String())
		r.mu.Unlock()
		if assistant == "" {
			assistant = "(no response)"
		}
		r.persist(agentsession.RoleAssistant, assistant, map[string]string{
			"type":       "assistant",
			"stopReason": string(stop),
		})
		r.broadcast(wsOut{Type: "done", StopReason: string(stop)})
	}
	r.finishTurn()
}

// finishTurn clears busy state and optionally starts a queued turn.
func (r *runner) finishTurn() {
	r.mu.Lock()
	r.busy = false
	r.reply.Reset()
	r.thought.Reset()
	r.thinkAt = time.Time{}
	if len(r.pending) > 0 {
		next := r.pending[0]
		r.pending = r.pending[1:]
		r.beginTurnLocked(next)
	}
	r.mu.Unlock()
	// Post-`done` snapshot: if a queued turn began, busy is already true again
	// and clients must not flip back to idle.
	r.broadcast(r.snapshot())
}

// onPermission is wired to the ACP runtime exactly once per runtime and
// outlives any individual WebSocket connection.  If no client is connected the
// permission is auto-cancelled so the turn is not stuck.
func (r *runner) onPermission(req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	reqID := string(req.ToolCall.ToolCallId)
	ch := make(chan string, 1)
	r.mu.Lock()
	r.perms[reqID] = &permWait{ch: ch}
	auto := r.auto
	r.mu.Unlock()

	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}
	r.flushThought()

	if auto {
		if opt := acpclient.PickOrdinaryAllow(req.Options); opt != "" {
			r.persist(agentsession.RolePermission, title+" · "+opt, agentsession.PermissionMeta{
				Type:      "permission",
				RequestID: reqID,
				Title:     title,
				OptionID:  opt,
				Outcome:   "auto",
				Options:   req.Options,
				ToolID:    reqID,
			})
			r.mu.Lock()
			delete(r.perms, reqID)
			r.mu.Unlock()
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId(opt)},
				},
			}, nil
		}
	}

	r.persist(agentsession.RolePermission, title, agentsession.PermissionMeta{
		Type:      "permission",
		RequestID: reqID,
		Title:     title,
		Outcome:   "requested",
		Options:   req.Options,
		ToolID:    reqID,
	})

	ticketID := ""
	if r.handler.Tickets != nil && r.session.AssistantID != "" {
		t, err := r.handler.Tickets.Create(r.ctx, r.session.UserID, assistticket.CreateInput{
			AssistantID:    r.session.AssistantID,
			SessionID:      r.session.ID,
			Kind:           assistticket.KindPermission,
			Title:          "需要确认：" + title,
			Reason:         "工具权限请求",
			ContextSummary: title,
			AskHuman:       "请选择允许一次、本会话记住，或拒绝",
			Payload: map[string]any{
				"requestId": reqID,
				"options":   req.Options,
				"toolTitle": title,
			},
		})
		if err == nil {
			ticketID = t.ID
		}
	}
	r.mu.Lock()
	if pw := r.perms[reqID]; pw != nil {
		pw.title = title
		pw.options = req.Options
		pw.ticketID = ticketID
	}
	nclients := len(r.clients)
	r.mu.Unlock()
	r.broadcast(wsOut{
		Type:      "permission_request",
		RequestID: reqID,
		Title:     title,
		Options:   req.Options,
		TicketID:  ticketID,
	})

	if nclients == 0 {
		r.cancelPermission(reqID, title, req.Options)
		r.mu.Lock()
		resp := r.permResult(reqID, "", true)
		r.mu.Unlock()
		return resp, nil
	}

	for {
		r.mu.Lock()
		nclients = len(r.clients)
		r.mu.Unlock()
		if nclients == 0 {
			r.cancelPermission(reqID, title, req.Options)
			r.mu.Lock()
			resp := r.permResult(reqID, "", true)
			r.mu.Unlock()
			return resp, nil
		}
		select {
		case opt := <-ch:
			r.persist(agentsession.RolePermission, title+" · "+opt, agentsession.PermissionMeta{
				Type:      "permission",
				RequestID: reqID,
				Title:     title,
				OptionID:  opt,
				Outcome:   "selected",
				Options:   req.Options,
				ToolID:    reqID,
			})
			r.broadcast(wsOut{Type: "permission_resolved", RequestID: reqID})
			r.mu.Lock()
			resp := r.permResult(reqID, opt, false)
			r.mu.Unlock()
			return resp, nil
		case <-time.After(300 * time.Millisecond):
			// Re-check whether a client is still connected.
		case <-r.ctx.Done():
			r.cancelPermission(reqID, title, req.Options)
			r.mu.Lock()
			resp := r.permResult(reqID, "", true)
			r.mu.Unlock()
			return resp, nil
		}
	}
}

// cancelPermission records a permission as cancelled and removes the wait channel.
// Must be called WITHOUT r.mu held.
func (r *runner) cancelPermission(reqID, title string, options any) {
	r.persist(agentsession.RolePermission, title+" · cancelled", agentsession.PermissionMeta{
		Type:      "permission",
		RequestID: reqID,
		Title:     title,
		Outcome:   "cancelled",
		Options:   options,
		ToolID:    reqID,
	})
	r.broadcast(wsOut{Type: "permission_resolved", RequestID: reqID})
	r.mu.Lock()
	delete(r.perms, reqID)
	r.mu.Unlock()
}

// permResult builds the response from the map and removes the entry.
// Must be called WITH r.mu held.
func (r *runner) permResult(reqID, optionID string, cancelled bool) acp.RequestPermissionResponse {
	delete(r.perms, reqID)
	if cancelled {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{},
			},
		}
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{
				OptionId: acp.PermissionOptionId(optionID),
			},
		},
	}
}

// answerPermission delivers the user's choice to the waiting permission handler.
func (r *runner) answerPermission(requestID, optionID string) {
	r.mu.Lock()
	pw := r.perms[requestID]
	if pw != nil {
		delete(r.perms, requestID)
	}
	r.mu.Unlock()
	if pw != nil {
		select {
		case pw.ch <- optionID:
		default:
		}
	}
}

// cancelTurn cancels the in-progress ACP prompt.
func (r *runner) cancelTurn() {
	_ = r.acp.Cancel(r.ctx, r.session.ID)
}

// setAuto toggles permission auto-approval for the runtime.
func (r *runner) setAuto(v bool) {
	r.mu.Lock()
	r.auto = v
	r.mu.Unlock()
	r.rt.SetAutoApprove(v)
}

// ──────────────────────────────────────────────────────────────────────
// Handler helper: manage runners map
// ──────────────────────────────────────────────────────────────────────

func (h *Handler) getRunner(id string) *runner {
	h.runnersMu.RLock()
	defer h.runnersMu.RUnlock()
	if h.runners == nil {
		return nil
	}
	return h.runners[id]
}

func (h *Handler) setRunner(id string, r *runner) {
	h.runnersMu.Lock()
	if h.runners == nil {
		h.runners = make(map[string]*runner)
	}
	h.runners[id] = r
	h.runnersMu.Unlock()
}

func (h *Handler) deleteRunner(id string) {
	h.runnersMu.Lock()
	r := h.runners[id]
	delete(h.runners, id)
	h.runnersMu.Unlock()
	if r != nil && r.cancel != nil {
		r.cancel()
	}
}

// runnerFor returns the runner for sess, creating one if necessary.
func (h *Handler) runnerFor(sess *agentsession.Session, user *storage.User) (*runner, error) {
	h.runnersMu.Lock()
	defer h.runnersMu.Unlock()

	if h.runners == nil {
		h.runners = make(map[string]*runner)
	}

	if r, ok := h.runners[sess.ID]; ok {
		if _, ok := h.ACP.Get(sess.ID); ok {
			return r, nil
		}
		// Runtime gone (stdio exec died / backend restart); tear down.
		if r.cancel != nil {
			r.cancel()
		}
		delete(h.runners, sess.ID)
	}

	provMeta, found := providers.ByID(h.ACP.Providers(), sess.ProviderID)
	if !found || !provMeta.Enabled {
		return nil, fmt.Errorf("unknown provider %q", sess.ProviderID)
	}
	if providers.NeedsSandbox(provMeta) && sess.SandboxID == "" {
		return nil, fmt.Errorf("agent runtime not running")
	}

	actor := manager.Actor{
		Username: user.Username,
		Role:     string(user.Role),
		APIKey:   user.APIKey,
	}

	rt, startErr := h.ACP.Start(context.Background(), sess.ID, sess.SandboxID, sess.ProviderID, manager.StartOpts{
		Actor:       actor,
		AutoApprove: true,
	})
	if startErr != nil {
		if existing, ok := h.ACP.Get(sess.ID); ok {
			rt = existing
		} else {
			return nil, fmt.Errorf("start agent: %w", startErr)
		}
	}
	if rt == nil {
		return nil, fmt.Errorf("agent runtime not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &runner{
		handler: h,
		session: sess,
		acp:     h.ACP,
		rt:      rt,
		ctx:     ctx,
		cancel:  cancel,
		clients: make(map[*wsClient]struct{}),
		auto:    true,
		perms:   make(map[string]*permWait),
	}
	rt.SetEventHandler(r.onEvent)
	rt.SetPermissionHandler(r.onPermission)
	rt.SetAutoApprove(true)
	h.runners[sess.ID] = r
	return r, nil
}
