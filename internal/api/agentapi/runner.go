package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/webpush"
)

// turnTimeout caps one prompt turn (thinking + tools + replies + permission
// waits). Multi-step coding work routinely exceeds 10 minutes; keep a safety
// bound so a hung LLM stream cannot occupy the concurrency gate forever.
const turnTimeout = 2 * time.Hour

func isDeadlineExceeded(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// ACP wraps the deadline as JSON-RPC -32603 Internal error.
	return strings.Contains(err.Error(), "context deadline exceeded")
}

func turnErrorMessage(err error) string {
	if isDeadlineExceeded(err) {
		return fmt.Sprintf("本轮工作超时（超过 %s），已停止。可拆成更小的任务再试。", formatTurnTimeout(turnTimeout))
	}
	return unwrapPromptErr(err)
}

func formatTurnTimeout(d time.Duration) string {
	if d%(time.Hour) == 0 && d >= time.Hour {
		h := int(d / time.Hour)
		if h == 1 {
			return "1 小时"
		}
		return fmt.Sprintf("%d 小时", h)
	}
	if d%(time.Minute) == 0 && d >= time.Minute {
		m := int(d / time.Minute)
		return fmt.Sprintf("%d 分钟", m)
	}
	return d.String()
}

// permWait tracks an outstanding interactive permission request that may outlive
// any single stream attachment.
type permWait struct {
	ch       chan string
	title    string
	options  any
	ticketID string

	// cancel is closed by /clear to abandon the dialog (the turn it belongs to
	// is being cancelled anyway); cancelOnce keeps the close idempotent.
	cancel     chan struct{}
	cancelOnce sync.Once
}

// pendingItem is a user message queued while a turn is in flight. ID is the
// persisted agent_messages row (used for pull-back and UI reconciliation);
// ClientMsgID is opaque to the server and echoes the sender's optimistic row.
type pendingItem struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	ClientMsgID string `json:"clientMsgId,omitempty"`
}

// runner owns the ACP turn lifecycle for one agent session: prompt execution,
// reply/thought buffering, permission handling, message persistence, and event
// fan-out to all connected stream clients (SSE). Closing a tab or switching
// tabs no longer interrupts generation; connections simply attach/detach.
type runner struct {
	handler *Handler
	session *agentsession.Session
	acp     *manager.Manager
	rt      *manager.Runtime
	// actor is the identity the runtime was started with; /clear reuses it to
	// restart the runtime.
	actor manager.Actor
	// profile is the ACP persona (coordinator / worker role / ""); kept so
	// /clear and reconnects restore the same tool surface.
	profile string

	ctx    context.Context
	cancel context.CancelFunc

	mu           sync.Mutex
	clients      map[*streamClient]struct{}
	busy         bool
	mode         string
	clearPending bool          // /clear arrived mid-turn; reset once the turn ends
	pending      []pendingItem // queued prompts while a turn is in progress
	queueVer     uint64        // monotonic queue-snapshot version (frame ordering)

	reply   strings.Builder
	thought strings.Builder
	thinkAt time.Time

	perms map[string]*permWait
}

// subscribe attaches a stream client to the runner's broadcast.
func (r *runner) subscribe(c *streamClient) {
	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.mu.Unlock()
}

// unsubscribe removes a stream client from the session and closes its sink.
func (r *runner) unsubscribe(c *streamClient) {
	r.mu.Lock()
	delete(r.clients, c)
	r.mu.Unlock()
	c.close()
}

func (r *runner) broadcast(v any) {
	r.mu.Lock()
	clients := make([]*streamClient, 0, len(r.clients))
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
	Pending []pendingItem `json:"pending,omitempty"`
	// Steer reports whether this runtime accepts mid-turn injection; false
	// tells the composer to queue instead.
	Steer    bool   `json:"steer"`
	QueueVer uint64 `json:"queueVersion"`
}

// queueSnapshot is the full queue state broadcast on every change. Clients
// keep the highest version so frames that arrive out of order are dropped.
type queueSnapshot struct {
	Type    string        `json:"type"` // "queue"
	Steer   bool          `json:"steer"`
	Version uint64        `json:"version"`
	Items   []pendingItem `json:"items"`
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
	items := make([]pendingItem, len(r.pending))
	copy(items, r.pending)
	s := statusSnapshot{
		Type:     "status",
		Busy:     r.busy,
		Reply:    r.reply.String(),
		Thought:  r.thought.String(),
		Pending:  items,
		Steer:    r.rt.SteerCapable(),
		QueueVer: r.queueVer,
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

// queueFrame builds a fresh queue snapshot; safe to call without the lock.
func (r *runner) queueFrame() queueSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.queueFrameLocked()
}

// queueFrameLocked bumps the snapshot version and copies the queue.
// Caller must hold r.mu.
func (r *runner) queueFrameLocked() queueSnapshot {
	r.queueVer++
	items := make([]pendingItem, len(r.pending))
	copy(items, r.pending)
	return queueSnapshot{
		Type:    "queue",
		Steer:   r.rt.SteerCapable(),
		Version: r.queueVer,
		Items:   items,
	}
}

// persist saves a row in the session transcript.
func (r *runner) persist(role, content string, meta any) {
	_, _ = r.handler.Store.AddMessage(r.ctx, r.session.ID, role, content, meta)
}

// persistUser writes a user row and returns its id ("" when unpersisted).
func (r *runner) persistUser(text string, meta any) string {
	msg, err := r.handler.Store.AddMessage(r.ctx, r.session.ID, agentsession.RoleUser, text, meta)
	if err != nil || msg == nil {
		return ""
	}
	return msg.ID
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
	r.broadcast(streamOut{Type: "event", Event: ev})
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
func (r *runner) prompt(text, clientMsgID string) {
	r.startUserTurn(text, map[string]string{"type": "user"}, clientMsgID)
}

// startUserTurn persists a user turn and runs it, or queues it while busy.
// meta carries the row's display information: plain prompts use
// {"type":"user"}, slash commands add the command name and the text the user
// typed (the persisted content is the expanded instruction text).
func (r *runner) startUserTurn(text string, meta any, clientMsgID string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	id := r.persistUser(text, meta)
	r.mu.Lock()
	if r.busy {
		r.pending = append(r.pending, pendingItem{ID: id, Text: text, ClientMsgID: clientMsgID})
		r.mu.Unlock()
		r.broadcast(r.queueFrame())
		return
	}
	r.beginTurnLocked(text)
	r.mu.Unlock()
}

// steer injects a user message into the running turn, falling back to the
// queue when the provider cannot steer or the turn ended under us. The row is
// persisted first, so neither path can lose it.
func (r *runner) steer(text, clientMsgID string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	id := r.persistUser(text, map[string]string{"type": "user"})
	r.mu.Lock()
	if !r.busy {
		// Nothing in flight to steer into: run it as a normal turn.
		r.beginTurnLocked(text)
		r.mu.Unlock()
		return
	}
	if err := r.rt.Steer(text); err == nil {
		r.mu.Unlock()
		return
	}
	// Unsupported provider, no active turn, or a full inbox: queue instead.
	r.pending = append(r.pending, pendingItem{ID: id, Text: text, ClientMsgID: clientMsgID})
	r.mu.Unlock()
	r.broadcast(r.queueFrame())
}

// unqueue pulls a queued message back before it runs. The store call runs
// under the lock so a message can never be drained into a turn between the
// queue removal and the cancelled marker.
func (r *runner) unqueue(messageID string, c *streamClient) {
	r.mu.Lock()
	idx := -1
	for i, it := range r.pending {
		if it.ID == messageID {
			idx = i
			break
		}
	}
	if idx < 0 {
		r.mu.Unlock()
		c.write(streamOut{Type: "error", Message: "消息已开始执行，无法撤回"})
		return
	}
	if err := r.handler.Store.CancelMessage(r.ctx, r.session.ID, messageID); err != nil {
		r.mu.Unlock()
		c.write(streamOut{Type: "error", Message: "撤回失败，请重试"})
		return
	}
	r.pending = append(r.pending[:idx], r.pending[idx+1:]...)
	r.mu.Unlock()
	r.broadcast(r.queueFrame())
}

func (r *runner) beginTurnLocked(text string) {
	r.busy = true
	r.reply.Reset()
	r.thought.Reset()
	r.thinkAt = time.Time{}
	go r.runTurn(text)
}

func (r *runner) runTurn(text string) {
	ctx, cancel := context.WithTimeout(r.ctx, turnTimeout)
	defer cancel()
	_ = r.executeTurn(ctx, text)
	r.finishTurn()
}

// promptAndWait persists a user turn and runs it synchronously so callers
// (project workers) can wait for completion while SSE clients see the same
// stream as an interactive chat.
func (r *runner) promptAndWait(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	r.persistUser(text, map[string]string{"type": "user"})
	r.mu.Lock()
	if r.busy {
		r.mu.Unlock()
		return fmt.Errorf("session busy")
	}
	r.busy = true
	r.reply.Reset()
	r.thought.Reset()
	r.thinkAt = time.Time{}
	r.mu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()
	err := r.executeTurn(runCtx, text)
	r.finishTurn()
	return err
}

func (r *runner) executeTurn(ctx context.Context, text string) error {
	// Tell connected clients a turn started (queued turn, other tabs) so the
	// composer locks immediately, not on the first streamed event.
	r.broadcast(r.snapshot())
	stop, err := r.acp.Prompt(ctx, r.session.ID, text)
	if err != nil {
		r.flushThought()
		// Client wait ended (timeout / cancel / transport); stop the in-process
		// agent so the next turn does not collide with leftover work.
		r.cancelTurn()
		msg := turnErrorMessage(err)
		r.persist(agentsession.RoleEvent, msg, map[string]string{"type": "error"})
		r.broadcast(streamOut{Type: "error", Message: msg})
		return err
	}
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
	r.broadcast(streamOut{Type: "done", StopReason: string(stop)})
	r.notifyTurnDone()
	return nil
}

// finishTurn clears busy state and optionally starts a queued turn. A pending
// /clear takes the turn slot instead: the runner stays busy across the reset
// and reset() calls back here once the fresh runtime is up.
func (r *runner) finishTurn() {
	r.mu.Lock()
	r.reply.Reset()
	r.thought.Reset()
	r.thinkAt = time.Time{}
	if r.clearPending {
		r.clearPending = false
		r.mu.Unlock()
		go r.reset()
		return
	}
	r.busy = false
	if len(r.pending) > 0 {
		next := r.pending[0]
		r.pending = r.pending[1:]
		r.beginTurnLocked(next.Text)
	}
	r.mu.Unlock()
	// Post-`done` snapshot: if a queued turn began, busy is already true again
	// and clients must not flip back to idle.
	r.broadcast(r.snapshot())
}

// autoResolve answers a permission request from the chat's permission mode.
// Interactive questions are never auto-answered: their options are the answer
// choices. ModeDecide runs automode.Resolve (L3 HardDeny, L2 confirm,
// classifier); SoftDeny escalates to the interactive dialog (handled=false).
// ModeAll / ModeEdit still apply L3 PreCheck before silent-allow. When SoftDeny
// escalates, title may be rewritten with rule/reason for the UI.
func (r *runner) autoResolve(req acp.RequestPermissionRequest, reqID string, title *string) (acp.RequestPermissionResponse, bool) {
	if title == nil {
		empty := ""
		title = &empty
	}
	if isQuestionRequest(req) {
		return acp.RequestPermissionResponse{}, false
	}
	// In-process agent already escalated SoftDeny/L2 — do not re-classify.
	if askHumanMeta(req) {
		return acp.RequestPermissionResponse{}, false
	}

	toolName := *title
	if req.ToolCall.Title != nil && strings.TrimSpace(*req.ToolCall.Title) != "" {
		toolName = *req.ToolCall.Title
	}
	kind := ""
	if req.ToolCall.Kind != nil {
		kind = string(*req.ToolCall.Kind)
	}

	switch r.permMode() {
	case automode.ModeAll:
		if resp, denied := r.hardDenyPermission(req, reqID, *title, toolName); denied {
			return resp, true
		}
		return r.allowPermission(req, reqID, *title, "")
	case automode.ModeEdit:
		if resp, denied := r.hardDenyPermission(req, reqID, *title, toolName); denied {
			return resp, true
		}
		if !automode.IsWorkspaceEdit(toolName, kind) {
			return acp.RequestPermissionResponse{}, false
		}
		return r.allowPermission(req, reqID, *title, "")
	default:
		res := r.resolvePermission(req, toolName)
		if res.EvalErr != nil && r.handler.Log != nil {
			r.handler.Log.Warn("auto mode classifier failed",
				"session", r.session.ID, "tool", toolName, "err", res.EvalErr)
		}

		rule := strings.TrimSpace(res.Verdict.Rule)
		reason := strings.TrimSpace(res.Verdict.Reason)

		if res.Verdict.Decision == automode.HardDeny {
			line := rule
			if reason != "" {
				line += " — " + reason
			}
			opt := acpclient.PickReject(req.Options)
			r.persist(agentsession.RolePermission, line, agentsession.PermissionMeta{
				Type:      "permission",
				RequestID: reqID,
				Title:     *title,
				OptionID:  opt,
				Outcome:   "auto_deny",
				Options:   req.Options,
				ToolID:    reqID,
				Rule:      rule,
				Reason:    reason,
			})
			r.mu.Lock()
			resp := r.permResult(reqID, opt, opt == "")
			r.mu.Unlock()
			return resp, true
		}

		if res.AskHuman {
			if rule != "" {
				annotated := *title + " · " + rule
				if reason != "" {
					annotated += " — " + reason
				}
				*title = annotated
			}
			return acp.RequestPermissionResponse{}, false
		}

		if !res.Allowed() {
			return acp.RequestPermissionResponse{}, false
		}
		return r.allowPermission(req, reqID, *title, rule)
	}
}

func (r *runner) hardDenyPermission(req acp.RequestPermissionRequest, reqID, title, toolName string) (acp.RequestPermissionResponse, bool) {
	args := ""
	if req.ToolCall.RawInput != nil {
		if b, err := json.Marshal(req.ToolCall.RawInput); err == nil {
			args = string(b)
		}
	}
	kind := ""
	if req.ToolCall.Kind != nil {
		kind = string(*req.ToolCall.Kind)
	}
	v, ok := automode.PreCheck(automode.Request{
		Name:  toolName,
		Title: title,
		Kind:  kind,
		Args:  args,
	})
	if !ok || v.Decision != automode.HardDeny {
		return acp.RequestPermissionResponse{}, false
	}
	rule := strings.TrimSpace(v.Rule)
	reason := strings.TrimSpace(v.Reason)
	line := rule
	if reason != "" {
		line += " — " + reason
	}
	opt := acpclient.PickReject(req.Options)
	r.persist(agentsession.RolePermission, line, agentsession.PermissionMeta{
		Type:      "permission",
		RequestID: reqID,
		Title:     title,
		OptionID:  opt,
		Outcome:   "auto_deny",
		Options:   req.Options,
		ToolID:    reqID,
		Rule:      rule,
		Reason:    reason,
	})
	r.mu.Lock()
	resp := r.permResult(reqID, opt, opt == "")
	r.mu.Unlock()
	return resp, true
}

func (r *runner) allowPermission(req acp.RequestPermissionRequest, reqID, title, rule string) (acp.RequestPermissionResponse, bool) {
	opt := acpclient.PickOrdinaryAllow(req.Options)
	if opt == "" {
		return acp.RequestPermissionResponse{}, false
	}
	label := permissionOptionLabel(req.Options, opt)
	if rule != "" {
		label = "自动审核通过 · " + rule
	}
	r.persist(agentsession.RolePermission, label, agentsession.PermissionMeta{
		Type:      "permission",
		RequestID: reqID,
		Title:     title,
		OptionID:  opt,
		Outcome:   "auto",
		Options:   req.Options,
		ToolID:    reqID,
		Rule:      rule,
	})
	r.mu.Lock()
	resp := r.permResult(reqID, opt, false)
	r.mu.Unlock()
	return resp, true
}

func askHumanMeta(req acp.RequestPermissionRequest) bool {
	v, _ := req.Meta[sysagent.PermissionMetaAskHumanKey].(bool)
	return v
}

// resolvePermission builds the classifier request and runs automode.Resolve.
func (r *runner) resolvePermission(req acp.RequestPermissionRequest, title string) automode.Resolution {
	args := ""
	if req.ToolCall.RawInput != nil {
		if b, err := json.Marshal(req.ToolCall.RawInput); err == nil {
			args = string(b)
		}
	}
	kind := ""
	if req.ToolCall.Kind != nil {
		kind = string(*req.ToolCall.Kind)
	}
	options := make([]string, 0, len(req.Options))
	for _, o := range req.Options {
		options = append(options, o.Name)
	}
	return automode.Resolve(r.ctx, r.handler.AutoMode, automode.Request{
		Name:       title,
		Title:      title,
		Kind:       kind,
		Args:       args,
		Options:    options,
		UserDigest: r.recentDigest(),
	})
}

// isQuestionRequest reports whether the request is an interactive question or
// plan approval rather than a tool action. The in-process agent marks these
// with a meta field because their titles carry the question text.
func isQuestionRequest(req acp.RequestPermissionRequest) bool {
	v, _ := req.Meta[sysagent.PermissionMetaKindKey].(string)
	return v == sysagent.PermissionKindQuestion
}

// recentDigest renders user/assistant turns for the classifier; tool results
// are excluded by RecentDigest so read content cannot steer the verdict.
func (r *runner) recentDigest() string {
	var digest []automode.DigestMessage
	if r.handler.Store != nil {
		if rows, err := r.handler.Store.ListRecentMessages(r.ctx, r.session.ID, 200); err == nil {
			for _, m := range agentsession.AfterLastClear(rows) {
				digest = append(digest, automode.DigestMessage{Role: m.Role, Content: m.Content})
			}
		}
	}
	r.mu.Lock()
	reply := strings.TrimSpace(r.reply.String())
	r.mu.Unlock()
	if reply != "" {
		digest = append(digest, automode.DigestMessage{Role: "assistant", Content: reply})
	}
	return automode.RecentDigest(digest, 4000)
}

// onPermission is wired to the ACP runtime exactly once per runtime and
// outlives any individual WebSocket connection. If no client is connected and
// the user has no Web Push subscription, the permission is auto-cancelled so
// the turn is not stuck. With a push subscription we keep waiting (turn
// timeout /clear / answer still apply) so the user can open the notification.
func (r *runner) onPermission(req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	reqID := string(req.ToolCall.ToolCallId)
	ch := make(chan string, 1)
	r.mu.Lock()
	r.perms[reqID] = &permWait{ch: ch, cancel: make(chan struct{})}
	r.mu.Unlock()

	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}
	r.flushThought()

	if resp, handled := r.autoResolve(req, reqID, &title); handled {
		return resp, nil
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
	r.broadcast(streamOut{
		Type:      "permission_request",
		RequestID: reqID,
		Title:     title,
		Options:   req.Options,
		TicketID:  ticketID,
	})
	r.notifyPermission(title)

	if nclients == 0 && !r.userHasPush() {
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
		if nclients == 0 && !r.userHasPush() {
			r.cancelPermission(reqID, title, req.Options)
			r.mu.Lock()
			resp := r.permResult(reqID, "", true)
			r.mu.Unlock()
			return resp, nil
		}
		select {
		case opt := <-ch:
			label := permissionOptionLabel(req.Options, opt)
			r.persist(agentsession.RolePermission, label, agentsession.PermissionMeta{
				Type:      "permission",
				RequestID: reqID,
				Title:     title,
				OptionID:  opt,
				Outcome:   "selected",
				Options:   req.Options,
				ToolID:    reqID,
			})
			r.broadcast(streamOut{Type: "permission_resolved", RequestID: reqID})
			r.mu.Lock()
			resp := r.permResult(reqID, opt, false)
			r.mu.Unlock()
			return resp, nil
		case <-time.After(300 * time.Millisecond):
			// Re-check whether a client is still connected (or push can cover).
		case <-r.permCancel(reqID):
			r.cancelPermission(reqID, title, req.Options)
			r.mu.Lock()
			resp := r.permResult(reqID, "", true)
			r.mu.Unlock()
			return resp, nil
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
	r.broadcast(streamOut{Type: "permission_resolved", RequestID: reqID})
	r.mu.Lock()
	pw := r.perms[reqID]
	ticketID := ""
	if pw != nil {
		ticketID = pw.ticketID
	}
	delete(r.perms, reqID)
	r.mu.Unlock()
	r.cancelTicket(ticketID)
}

// cancelTicket closes the assist ticket behind an abandoned permission request
// so the console todo list never keeps an item nobody can act on.
func (r *runner) cancelTicket(ticketID string) {
	if ticketID == "" || r.handler.Tickets == nil {
		return
	}
	// The runner ctx may already be done (shutdown, session delete), but the
	// ticket should still be closed.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 5*time.Second)
	defer cancel()
	if _, err := r.handler.Tickets.Cancel(ctx, ticketID, "权限请求已取消"); err != nil && r.handler.Log != nil {
		r.handler.Log.Warn("cancel assist ticket", "ticket", ticketID, "err", err)
	}
}

func (r *runner) userHasPush() bool {
	if r.handler == nil || r.handler.Push == nil || r.session == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 2*time.Second)
	defer cancel()
	return r.handler.Push.HasSubscription(ctx, r.session.UserID)
}

func (r *runner) notifyPermission(toolTitle string) {
	if r.handler == nil || r.handler.Push == nil || r.session == nil {
		return
	}
	title := "需要确认"
	body := strings.TrimSpace(toolTitle)
	if body == "" {
		body = "助手请求权限"
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 5*time.Second)
	defer cancel()
	r.handler.Push.Notify(ctx, r.session.UserID, webpush.Payload{
		Type:  "permission",
		Title: title,
		Body:  body,
		URL:   webpush.SessionURL(r.session.AssistantID, r.session.ID),
	})
}

func (r *runner) notifyTurnDone() {
	if r.handler == nil || r.handler.Push == nil || r.session == nil {
		return
	}
	sessTitle := webpush.TruncateTitle(r.session.Title, 48)
	body := "回合已完成"
	if sessTitle != "" {
		body = sessTitle + " · 回合已完成"
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 5*time.Second)
	defer cancel()
	r.handler.Push.Notify(ctx, r.session.UserID, webpush.Payload{
		Type:  "turn_done",
		Title: "会话完成",
		Body:  body,
		URL:   webpush.SessionURL(r.session.AssistantID, r.session.ID),
	})
}

// permCancel returns the /clear abandonment channel for a pending permission
// request. A missing entry yields nil (a channel that never fires), which is
// exactly the "no dialog to cancel" semantics the select needs.
func (r *runner) permCancel(reqID string) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pw := r.perms[reqID]; pw != nil {
		return pw.cancel
	}
	return nil
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
	r.resolveTicketForAnswer(pw, optionID)
}

// resolveTicketForAnswer closes the assist ticket behind an answered permission
// request. The browser also resolves it over HTTP, but the runner is the
// authority on the decision: a dropped request (tab closed right after the
// click) must not leave the ticket pending with nobody left to answer it.
func (r *runner) resolveTicketForAnswer(pw *permWait, optionID string) {
	if pw == nil || pw.ticketID == "" || r.handler.Tickets == nil {
		return
	}
	resolution := assistticket.ResAllowOnce
	if optionRejects(pw.options, optionID) {
		resolution = assistticket.ResReject
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 5*time.Second)
	defer cancel()
	if _, err := r.handler.Tickets.Resolve(ctx, pw.ticketID, resolution, optionID); err != nil && r.handler.Log != nil {
		r.handler.Log.Warn("resolve assist ticket", "ticket", pw.ticketID, "err", err)
	}
}

// optionRejects reports whether optionID is one of the request's reject
// options, falling back to the option id when they are not ACP options.
func optionRejects(options any, optionID string) bool {
	opts, ok := options.([]acp.PermissionOption)
	if !ok {
		return strings.Contains(optionID, "reject")
	}
	for _, o := range opts {
		if string(o.OptionId) != optionID {
			continue
		}
		return o.Kind == acp.PermissionOptionKindRejectOnce ||
			o.Kind == acp.PermissionOptionKindRejectAlways
	}
	return strings.Contains(optionID, "reject")
}

// permissionOptionLabel resolves an ACP option id to its user-facing name so
// transcript rows show "继续用代理" instead of "opt-0".
func permissionOptionLabel(options any, optionID string) string {
	if optionID == "" {
		return ""
	}
	opts, ok := options.([]acp.PermissionOption)
	if ok {
		for _, o := range opts {
			if string(o.OptionId) != optionID {
				continue
			}
			if name := strings.TrimSpace(o.Name); name != "" {
				return name
			}
			break
		}
	}
	return optionID
}

// cancelTurn cancels the in-progress ACP prompt.
func (r *runner) cancelTurn() {
	_ = r.acp.Cancel(r.ctx, r.session.ID)
}

func (r *runner) permMode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return automode.NormalizeMode(r.mode)
}

// setPermMode selects how tool calls are approved for this chat.
func (r *runner) setPermMode(mode string) {
	mode = automode.NormalizeMode(mode)
	r.mu.Lock()
	r.mode = mode
	rt := r.rt
	r.mu.Unlock()
	if rt != nil {
		rt.SetPermMode(mode)
	}
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

	profile := h.profileFor(context.Background(), user, sess)

	if r, ok := h.runners[sess.ID]; ok {
		if rt, ok := h.ACP.Get(sess.ID); ok && rt.Profile == profile {
			return r, nil
		}
		// Runtime gone or wrong persona; tear down and recreate.
		if r.cancel != nil {
			r.cancel()
		}
		delete(h.runners, sess.ID)
		if _, ok := h.ACP.Get(sess.ID); ok {
			h.ACP.Stop(sess.ID)
		}
	} else if rt, ok := h.ACP.Get(sess.ID); ok && rt.Profile != profile {
		h.ACP.Stop(sess.ID)
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
		AssistantID: sess.AssistantID,
		Project:     h.projectStart(context.Background(), user, sess, profile),
	})
	if startErr != nil {
		if existing, ok := h.ACP.Get(sess.ID); ok {
			rt = existing
			profile = existing.Profile
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
		actor:   actor,
		profile: profile,
		ctx:     ctx,
		cancel:  cancel,
		clients: make(map[*streamClient]struct{}),
		mode:    automode.ModeDecide,
		perms:   make(map[string]*permWait),
	}
	rt.SetEventHandler(r.onEvent)
	rt.SetPermissionHandler(r.onPermission)
	rt.SetPermMode(automode.ModeDecide)
	h.runners[sess.ID] = r
	return r, nil
}

// unwrapPromptErr peels ACP JSON-RPC Internal error wrappers so the chat UI
// sees the actionable message (e.g. LLM not configured) instead of raw JSON.
func unwrapPromptErr(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, sysagent.ErrLLMNotConfigured) {
		return sysagent.ErrLLMNotConfigured.Error()
	}
	if errors.Is(err, sysagent.ErrFreeQuotaExceeded) {
		return sysagent.ErrFreeQuotaExceeded.Error()
	}
	var re *acp.RequestError
	if errors.As(err, &re) && re != nil {
		switch data := re.Data.(type) {
		case map[string]any:
			if s, ok := data["error"].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
			if s, ok := data["details"].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		case string:
			if strings.TrimSpace(data) != "" {
				return data
			}
		}
	}
	msg := err.Error()
	// Last resort: parse a marshalled RequestError string.
	var parsed struct {
		Data struct {
			Error string `json:"error"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(msg), &parsed) == nil && strings.TrimSpace(parsed.Data.Error) != "" {
		return parsed.Data.Error
	}
	return msg
}
