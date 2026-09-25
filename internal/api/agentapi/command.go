package agentapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/commands"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// skillQueryTimeout bounds the container execs behind skill listing/reading; a
// slow sandbox degrades to built-ins instead of hanging the command path.
const skillQueryTimeout = 5 * time.Second

// listCommands serves the slash command catalog: built-in actions and skills
// always, installed skills when the agent environment is already running.
func (h *Handler) listCommands(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	sess, err := h.Store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, agentsession.ErrNotFound) {
		httpx.WriteErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil || (sess.UserID != user.Username && user.Role != "admin") {
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"commands": h.commandCatalog(r.Context(), sess)})
}

// commandCatalog assembles the catalog. Built-in actions and skills are
// compile-time; installed skills are best-effort and only listed while the
// agent environment runs — listing must never provision a container.
func (h *Handler) commandCatalog(ctx context.Context, sess *agentsession.Session) []commands.Command {
	out := commands.Actions()
	for _, s := range tools.DefaultSkills() {
		desc := s.Description
		if zh, ok := commands.SkillDisplayDescription(s.Name); ok {
			desc = zh
		}
		out = append(out, commands.Command{
			Name:        s.Name,
			Kind:        commands.KindSkill,
			Source:      commands.SourceBuiltin,
			Description: desc,
			Args:        s.Params,
		})
	}
	for _, s := range h.installedSkills(ctx, sess) {
		desc := strings.Join(strings.Fields(s.Description), " ")
		if desc == "" {
			desc = "已安装技能"
		}
		out = append(out, commands.Command{
			Name:        s.Name,
			Kind:        commands.KindSkill,
			Source:      commands.SourceInstalled,
			Description: desc,
			Args:        s.Params,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := kindRank(out[i].Kind), kindRank(out[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func kindRank(k commands.Kind) int {
	if k == commands.KindAction {
		return 0
	}
	return 1
}

// installedSkills lists skills from the session owner's agent home. Any
// failure (no environment, exec error) degrades to "none listed".
func (h *Handler) installedSkills(ctx context.Context, sess *agentsession.Session) []tools.Skill {
	binder, ok := h.agentBinder()
	if !ok {
		return nil
	}
	if !h.agentSlotRunning(ctx, sess.UserID) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, skillQueryTimeout)
	defer cancel()
	skills, err := tools.ListInstalledSkills(ctx, binder, tools.Actor{Username: sess.UserID})
	if err != nil {
		if h.Log != nil {
			h.Log.Warn("list installed skills", "session", sess.ID, "err", err)
		}
		return nil
	}
	return skills
}

func (h *Handler) agentBinder() (*tools.AgentBinder, bool) {
	if h.Envs == nil || h.Sandboxes == nil {
		return nil, false
	}
	return &tools.AgentBinder{Slots: h.Envs, Exec: h.Sandboxes, Files: h.Sandboxes}, true
}

func (h *Handler) agentSlotRunning(ctx context.Context, userID string) bool {
	if h.Envs == nil {
		return false
	}
	views, err := h.Envs.List(ctx, userID)
	if err != nil {
		return false
	}
	for _, v := range views {
		if v.Slot == userenv.SlotAgent {
			return v.Status == string(sandbox.StatusRunning)
		}
	}
	return false
}

// command executes a slash command sent by the composer. Skills expand into
// the same instruction text the model-invoked Skill tool produces and run as a
// normal user turn; /help and /clear act on the control plane directly.
func (r *runner) command(name, args string) {
	name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "/")))
	if name == "" {
		return
	}
	switch name {
	case "clear":
		r.requestClear()
		return
	case "help":
		r.help()
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, skillQueryTimeout)
	text, err := r.expandSkill(ctx, name, args)
	cancel()
	if err != nil {
		// Unknown or unreadable commands land in the transcript as a system
		// bubble; sending a WS "error" frame would flip the connection chrome.
		r.broadcast(wsOut{Type: "event", Event: acpclient.Event{
			Type: "command_error",
			Text: "命令 " + name + " 执行失败：" + err.Error(),
		}})
		return
	}
	r.startUserTurn(text, map[string]any{
		"type":        "user",
		"command":     name,
		"commandArgs": strings.TrimSpace(args),
		"display":     commandDisplay(name, args),
	}, "")
}

// expandSkill resolves name against the session owner's agent home when that
// environment is running, falling back to built-ins.
func (r *runner) expandSkill(ctx context.Context, name, args string) (string, error) {
	binder, ok := r.handler.agentBinder()
	if ok && r.handler.agentSlotRunning(ctx, r.session.UserID) {
		return tools.ExpandInstalledSkill(ctx, binder, tools.Actor{Username: r.session.UserID}, name, args)
	}
	return tools.ExpandSkill(name, args)
}

func commandDisplay(name, args string) string {
	out := "/" + name
	if trimmed := strings.TrimSpace(args); trimmed != "" {
		out += " " + trimmed
	}
	return out
}

// help renders the command catalog into the transcript without calling the model.
func (r *runner) help() {
	text := commands.HelpText(r.handler.commandCatalog(r.ctx, r.session))
	r.persist(agentsession.RoleEvent, text, map[string]string{"type": "event", "command": "help"})
	r.broadcast(wsOut{Type: "event", Event: acpclient.Event{Type: "command_result", Text: text}})
}

// requestClear starts a context reset. A turn in flight is cancelled and the
// reset deferred to finishTurn; either way the runner stays busy across the
// reset so queued prompts run against the fresh runtime.
func (r *runner) requestClear() {
	r.mu.Lock()
	busy := r.busy
	var dropped []pendingItem
	if busy {
		r.clearPending = true
		dropped = r.pending
		r.pending = nil
	} else {
		r.busy = true
	}
	// Any interactive permission dialog belongs to the turn being abandoned;
	// closing its channel unwedges onPermission (runner.go select).
	for _, pw := range r.perms {
		pw.cancelOnce.Do(func() { close(pw.cancel) })
	}
	r.mu.Unlock()
	if busy {
		// The reset abandons the queue; mark the rows cancelled so model
		// context projections skip them.
		for _, it := range dropped {
			_ = r.handler.Store.CancelMessage(r.ctx, r.session.ID, it.ID)
		}
		if len(dropped) > 0 {
			r.broadcast(r.queueFrame())
		}
		_ = r.acp.Cancel(r.ctx, r.session.ID)
		return
	}
	go r.reset()
}

// reset appends the /clear marker, tells clients to refresh, and rebuilds the
// runtime so provider-side state (stdio subprocess conversation, sysagent
// plan/permission gates) starts clean. Nothing is deleted: the transcript
// keeps every row and the model context restarts after the marker.
func (r *runner) reset() {
	r.broadcast(r.snapshot())
	r.persist(agentsession.RoleEvent, "上下文已清空", map[string]string{"type": agentsession.MetaTypeClear})
	r.broadcast(wsOut{Type: "cleared"})

	rt, err := r.restartRuntime()
	if err != nil {
		if r.handler.Log != nil {
			r.handler.Log.Warn("clear: restart agent runtime", "session", r.session.ID, "err", err)
		}
		r.broadcast(wsOut{Type: "error", Message: "清空后重启 agent 失败：" + err.Error()})
	} else {
		r.mu.Lock()
		r.rt = rt
		r.mu.Unlock()
	}
	r.finishTurn()
}

// restartRuntime replaces the ACP runtime for the session, re-wiring the
// per-runtime handlers the runner owns.
func (r *runner) restartRuntime() (*manager.Runtime, error) {
	r.acp.Stop(r.session.ID)
	rt, err := r.acp.Start(r.ctx, r.session.ID, r.session.SandboxID, r.session.ProviderID, manager.StartOpts{Actor: r.actor})
	if err != nil {
		return nil, err
	}
	rt.SetEventHandler(r.onEvent)
	rt.SetPermissionHandler(r.onPermission)
	rt.SetAutoMode(r.autoEnabled())
	return rt, nil
}

func (r *runner) autoEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.auto
}
