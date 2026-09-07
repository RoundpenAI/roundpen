// Package agentapi serves Agent Web UI REST + WebSocket endpoints.
package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/gorilla/websocket"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/browsetask"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Handler serves /v1/agents and /v1/agent-sessions.
type Handler struct {
	Log         *slog.Logger
	Store       *agentsession.Store
	ACP         *manager.Manager
	Provisioner *agentenv.Provisioner
	Sandboxes   sandbox.Manager
	LLMGW       *llmgw.Gateway
	Hub         *browser.Hub
	Envs        *userenv.Service
	Tasks       *browsetask.Store
	DestroySbx  bool // delete sandbox on session delete
}

// Mount registers routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents", h.listAgents)
	mux.HandleFunc("GET /v1/agent-sessions", h.listSessions)
	mux.HandleFunc("POST /v1/agent-sessions", h.createSession)
	mux.HandleFunc("GET /v1/agent-sessions/{id}", h.getSession)
	mux.HandleFunc("DELETE /v1/agent-sessions/{id}", h.deleteSession)
	mux.HandleFunc("GET /v1/agent-sessions/{id}/messages", h.listMessages)
	mux.HandleFunc("GET /v1/agent-sessions/{id}/ws", h.sessionWS)
	h.mountBrowser(mux)
	h.mountTasks(mux)
}

func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"agents": h.ACP.Providers()})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	list, err := h.Store.ListByUser(r.Context(), user.Username, 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*agentsession.Session{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

type createReq struct {
	Title      string `json:"title"`
	ProviderID string `json:"providerId"`
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req createReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ProviderID == "" {
		req.ProviderID = "sysadmin"
	}
	if req.Title == "" {
		req.Title = "New chat"
	}

	provMeta, ok := providers.ByID(h.ACP.Providers(), req.ProviderID)
	if !ok || !provMeta.Enabled {
		writeErr(w, http.StatusBadRequest, "unknown provider")
		return
	}

	// Create DB row first so session id is stable for injection.
	sess, err := h.Store.Create(r.Context(), user.Username, req.Title, req.ProviderID, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	sandboxID := ""
	if providers.NeedsSandbox(provMeta) {
		vkey := h.pickVirtualKey(r.Context())
		prov := *h.Provisioner
		prov.Config.APIKey = user.APIKey
		prov.Config.VirtualKey = vkey
		if provMeta.TemplateID != "" {
			prov.Config.TemplateID = provMeta.TemplateID
		}
		if h.LLMGW != nil {
			prov.Config.DefaultModel = h.LLMGW.DefaultModel()
		}
		res, err := prov.Provision(r.Context(), sess.ID, req.ProviderID, user.Username)
		if err != nil {
			_ = h.Store.Delete(r.Context(), sess.ID)
			writeErr(w, http.StatusBadGateway, "provision sandbox: "+err.Error())
			return
		}
		_ = h.Store.UpdateSandbox(r.Context(), sess.ID, res.Sandbox.ID)
		sess.SandboxID = res.Sandbox.ID
		sandboxID = res.Sandbox.ID
	}

	actor := manager.Actor{
		Username: user.Username,
		Role:     string(user.Role),
		APIKey:   user.APIKey,
	}
	if _, err := h.ACP.Start(r.Context(), sess.ID, sandboxID, req.ProviderID, manager.StartOpts{
		Actor: actor,
	}); err != nil {
		writeErr(w, http.StatusBadGateway, "start agent: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (h *Handler) pickVirtualKey(ctx context.Context) string {
	if h.LLMGW == nil {
		return ""
	}
	keys, err := h.LLMGW.Store().ListVirtualKeys(ctx)
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

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	sess, err := h.Store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, agentsession.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess.UserID != user.Username && user.Role != "admin" {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	sess, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, agentsession.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess.UserID != user.Username && user.Role != "admin" {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	if h.ACP != nil {
		h.ACP.Stop(id)
	}
	if err := h.Store.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.DestroySbx && sess.SandboxID != "" && h.Sandboxes != nil {
		_ = h.Sandboxes.Delete(r.Context(), sess.SandboxID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	sess, err := h.Store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, agentsession.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil || (sess.UserID != user.Username && user.Role != "admin") {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	msgs, err := h.Store.ListMessages(r.Context(), sess.ID, 2000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if msgs == nil {
		msgs = []*agentsession.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

type wsIn struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	OptionID  string `json:"optionId,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	Enabled   bool   `json:"enabled"`
}

type wsOut struct {
	Type       string `json:"type"`
	Event      any    `json:"event,omitempty"`
	Message    string `json:"message,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Title      string `json:"title,omitempty"`
	Options    any    `json:"options,omitempty"`
}

func (h *Handler) sessionWS(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	sess, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, agentsession.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil || (sess.UserID != user.Username && user.Role != "admin") {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}

	rt, ok := h.ACP.Get(id)
	if !ok {
		actor := manager.Actor{
			Username: user.Username,
			Role:     string(user.Role),
			APIKey:   user.APIKey,
		}
		provMeta, found := providers.ByID(h.ACP.Providers(), sess.ProviderID)
		if !found {
			writeErr(w, http.StatusConflict, "unknown provider")
			return
		}
		if providers.NeedsSandbox(provMeta) && sess.SandboxID == "" {
			writeErr(w, http.StatusConflict, "agent runtime not running")
			return
		}
		var startErr error
		rt, startErr = h.ACP.Start(r.Context(), sess.ID, sess.SandboxID, sess.ProviderID, manager.StartOpts{Actor: actor, AutoApprove: true})
		if startErr != nil {
			writeErr(w, http.StatusBadGateway, "restart agent: "+startErr.Error())
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var writeMu sync.Mutex
	write := func(v any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = conn.WriteJSON(v)
	}

	const (
		wsPongWait  = 60 * time.Second
		wsPingEvery = 30 * time.Second
	)
	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		t := time.NewTicker(wsPingEvery)
		defer t.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-t.C:
				writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
				if err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()

	permCh := make(map[string]chan string)
	var permMu sync.Mutex
	autoMu := sync.Mutex{}
	autoMode := true
	rt.SetAutoApprove(true)

	var turnMu sync.Mutex
	var replyBuf strings.Builder
	var thoughtBuf strings.Builder
	var thoughtStart time.Time

	persist := func(role, content string, meta any) {
		_, _ = h.Store.AddMessage(context.Background(), sess.ID, role, content, meta)
	}
	flushThought := func() {
		turnMu.Lock()
		text := strings.TrimSpace(thoughtBuf.String())
		thoughtBuf.Reset()
		var dur int64
		if !thoughtStart.IsZero() {
			dur = time.Since(thoughtStart).Milliseconds()
			thoughtStart = time.Time{}
		}
		turnMu.Unlock()
		if text == "" {
			return
		}
		persist(agentsession.RoleThought, text, map[string]any{
			"type":       "thought",
			"durationMs": dur,
		})
	}
	isAuto := func() bool {
		autoMu.Lock()
		defer autoMu.Unlock()
		return autoMode
	}

	rt.SetEventHandler(func(ev acpclient.Event) {
		write(wsOut{Type: "event", Event: ev})
		switch ev.Type {
		case "agent_message":
			if ev.Text == "" {
				return
			}
			flushThought()
			turnMu.Lock()
			replyBuf.WriteString(ev.Text)
			turnMu.Unlock()
		case "agent_thought":
			if ev.Text == "" {
				return
			}
			turnMu.Lock()
			if thoughtBuf.Len() == 0 {
				thoughtStart = time.Now()
			}
			thoughtBuf.WriteString(ev.Text)
			turnMu.Unlock()
		case "tool_call", "tool_call_update":
			flushThought()
			_, _ = h.Store.UpsertToolMessage(context.Background(), sess.ID, agentsession.ToolMeta{
				Type:   "tool_call",
				ToolID: ev.ToolID,
				Title:  ev.Title,
				Status: ev.Status,
				Kind:   ev.Kind,
				Input:  ev.Input,
				Output: ev.Output,
			})
		case "plan":
			flushThought()
			text := strings.TrimSpace(ev.Text)
			if text == "" {
				text = "plan"
			}
			persist(agentsession.RoleEvent, text, map[string]string{"type": "plan"})
		case "permission":
			flushThought()
			title := strings.TrimSpace(ev.Title)
			if title == "" {
				title = "tool"
			}
			outcome := ev.Status
			if outcome == "" {
				outcome = "auto"
			}
			persist(agentsession.RolePermission, title+" · "+ev.Text, agentsession.PermissionMeta{
				Type:     "permission",
				Title:    title,
				OptionID: ev.Text,
				Outcome:  outcome,
				ToolID:   ev.ToolID,
			})
		}
	})

	rt.SetPermissionHandler(func(req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		reqID := string(req.ToolCall.ToolCallId)
		ch := make(chan string, 1)
		permMu.Lock()
		permCh[reqID] = ch
		permMu.Unlock()
		title := ""
		if req.ToolCall.Title != nil {
			title = *req.ToolCall.Title
		}
		flushThought()
		if isAuto() {
			if opt := acpclient.PickOrdinaryAllow(req.Options); opt != "" {
				persist(agentsession.RolePermission, title+" · "+opt, agentsession.PermissionMeta{
					Type:      "permission",
					RequestID: reqID,
					Title:     title,
					OptionID:  opt,
					Outcome:   "auto",
					Options:   req.Options,
					ToolID:    reqID,
				})
				return acp.RequestPermissionResponse{
					Outcome: acp.RequestPermissionOutcome{
						Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId(opt)},
					},
				}, nil
			}
		}
		persist(agentsession.RolePermission, title, agentsession.PermissionMeta{
			Type:      "permission",
			RequestID: reqID,
			Title:     title,
			Outcome:   "requested",
			Options:   req.Options,
			ToolID:    reqID,
		})
		write(wsOut{Type: "permission_request", RequestID: reqID, Title: title, Options: req.Options})
		select {
		case opt := <-ch:
			persist(agentsession.RolePermission, title+" · "+opt, agentsession.PermissionMeta{
				Type:      "permission",
				RequestID: reqID,
				Title:     title,
				OptionID:  opt,
				Outcome:   "selected",
				Options:   req.Options,
				ToolID:    reqID,
			})
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId(opt)},
				},
			}, nil
		case <-r.Context().Done():
			persist(agentsession.RolePermission, title+" · cancelled", agentsession.PermissionMeta{
				Type:      "permission",
				RequestID: reqID,
				Title:     title,
				Outcome:   "cancelled",
				Options:   req.Options,
				ToolID:    reqID,
			})
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
			}, r.Context().Err()
		}
	})

	for {
		var in wsIn
		if err := conn.ReadJSON(&in); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		switch strings.ToLower(in.Type) {
		case "prompt":
			text := strings.TrimSpace(in.Text)
			if text == "" {
				continue
			}
			persist(agentsession.RoleUser, text, map[string]string{"type": "user"})
			go func(t string) {
				turnMu.Lock()
				replyBuf.Reset()
				thoughtBuf.Reset()
				turnMu.Unlock()

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				stop, err := h.ACP.Prompt(ctx, sess.ID, t)
				if err != nil {
					flushThought()
					persist(agentsession.RoleEvent, err.Error(), map[string]string{"type": "error"})
					write(wsOut{Type: "error", Message: err.Error()})
					return
				}
				flushThought()
				turnMu.Lock()
				assistant := strings.TrimSpace(replyBuf.String())
				turnMu.Unlock()
				if assistant == "" {
					assistant = "(no response)"
				}
				persist(agentsession.RoleAssistant, assistant, map[string]string{
					"type":       "assistant",
					"stopReason": string(stop),
				})
				write(wsOut{Type: "done", StopReason: string(stop)})
			}(text)
		case "cancel":
			_ = h.ACP.Cancel(r.Context(), sess.ID)
		case "auto":
			autoMu.Lock()
			autoMode = in.Enabled
			autoMu.Unlock()
			rt.SetAutoApprove(in.Enabled)
		case "permission":
			permMu.Lock()
			ch := permCh[in.RequestID]
			delete(permCh, in.RequestID)
			permMu.Unlock()
			if ch != nil {
				select {
				case ch <- in.OptionID:
				default:
				}
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}
