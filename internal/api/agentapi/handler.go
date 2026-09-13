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

	"github.com/gorilla/websocket"

	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/browsetask"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
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
	Tickets     *assistticket.Store
	DestroySbx  bool // delete sandbox on session delete

	runnersMu sync.RWMutex
	runners   map[string]*runner
}

// Mount registers routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents", h.listAgents)
	mux.HandleFunc("GET /v1/agent-sessions", h.listSessions)
	mux.HandleFunc("POST /v1/agent-sessions", h.createSession)
	mux.HandleFunc("GET /v1/agent-sessions/{id}", h.getSession)
	mux.HandleFunc("PATCH /v1/agent-sessions/{id}", h.renameSession)
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
	Title       string `json:"title"`
	ProviderID  string `json:"providerId"`
	AssistantID string `json:"assistantId"`
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

	sess, err := h.startSession(r.Context(), user, req.Title, req.ProviderID, req.AssistantID)
	if err != nil {
		writeSessionStartErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

type sessionStartError struct {
	Code int
	Msg  string
}

func (e *sessionStartError) Error() string { return e.Msg }

func writeSessionStartErr(w http.ResponseWriter, err error) {
	var se *sessionStartError
	if errors.As(err, &se) {
		writeErr(w, se.Code, se.Msg)
		return
	}
	writeErr(w, http.StatusInternalServerError, err.Error())
}

// DefaultAssistantProvider is the ACP provider used for new assistant chats.
// sysadmin runs in-process (no sandbox). Swap back to "claude" when QEMU coding agents are the default again.
const DefaultAssistantProvider = "sysadmin"

// StartForAssistant creates or is used by assistants ensure-session.
func (h *Handler) StartForAssistant(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error) {
	if title == "" {
		title = "Chat"
	}
	return h.startSession(ctx, user, title, DefaultAssistantProvider, assistantID)
}

func (h *Handler) startSession(ctx context.Context, user *storage.User, title, providerID, assistantID string) (*agentsession.Session, error) {
	provMeta, ok := providers.ByID(h.ACP.Providers(), providerID)
	if !ok || !provMeta.Enabled {
		return nil, &sessionStartError{Code: http.StatusBadRequest, Msg: "unknown provider"}
	}

	sess, err := h.Store.Create(ctx, user.Username, title, providerID, "", assistantID)
	if err != nil {
		return nil, err
	}

	sandboxID := ""
	if providers.NeedsSandbox(provMeta) {
		vkey := h.pickVirtualKey(ctx)
		prov := *h.Provisioner
		prov.Config.APIKey = user.APIKey
		prov.Config.VirtualKey = vkey
		if provMeta.TemplateID != "" {
			prov.Config.TemplateID = provMeta.TemplateID
		}
		if h.LLMGW != nil {
			prov.Config.DefaultModel = h.LLMGW.DefaultModel()
		}
		res, err := prov.Provision(ctx, sess.ID, providerID, user.Username)
		if err != nil {
			_ = h.Store.Delete(ctx, sess.ID)
			return nil, &sessionStartError{Code: http.StatusBadGateway, Msg: "provision sandbox: " + err.Error()}
		}
		_ = h.Store.UpdateSandbox(ctx, sess.ID, res.Sandbox.ID)
		sess.SandboxID = res.Sandbox.ID
		sandboxID = res.Sandbox.ID
	}

	actor := manager.Actor{
		Username: user.Username,
		Role:     string(user.Role),
		APIKey:   user.APIKey,
	}
	if _, err := h.ACP.Start(ctx, sess.ID, sandboxID, providerID, manager.StartOpts{
		Actor: actor,
	}); err != nil {
		return nil, &sessionStartError{Code: http.StatusBadGateway, Msg: "start agent: " + err.Error()}
	}
	return sess, nil
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
	h.deleteRunner(id)
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

func (h *Handler) renameSession(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeErr(w, http.StatusBadRequest, "title required")
		return
	}
	if err := h.Store.Rename(r.Context(), id, req.Title); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	sess.Title = req.Title
	writeJSON(w, http.StatusOK, sess)
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
	TicketID   string `json:"ticketId,omitempty"`
}

// wsClient wraps a browser WebSocket so the runner can fan out to many
// connections without sharing one write mutex.
type wsClient struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool
}

func (c *wsClient) write(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteJSON(v); err != nil {
		c.closed = true
		_ = c.conn.Close()
	}
}

func (c *wsClient) ping() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		c.closed = true
		_ = c.conn.Close()
	}
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

	// Fail fast with a clear 4xx before upgrading when the runtime can't be
	// restarted (unknown provider / missing sandbox).
	if _, ok := h.ACP.Get(id); !ok {
		provMeta, found := providers.ByID(h.ACP.Providers(), sess.ProviderID)
		if !found {
			writeErr(w, http.StatusConflict, "unknown provider")
			return
		}
		if providers.NeedsSandbox(provMeta) && sess.SandboxID == "" {
			writeErr(w, http.StatusConflict, "agent runtime not running")
			return
		}
	}

	// Upgrade before Start so the browser leaves CONNECTING quickly.  A
	// slow/hanging ACP Start used to block the handshake entirely.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &wsClient{conn: conn}

	// Push a frame immediately so reverse proxies flush the 101 upgrade
	// before ACP Start (which can take seconds) runs on this goroutine.
	client.write(wsOut{Type: "hello", Message: "ok"})

	// The runner owns the turn lifecycle independent of this connection: a
	// dropped or closed browser tab must not stop generation.  Attaching just
	// mirrors its stream.
	run, runErr := h.runnerFor(sess, user)
	if runErr != nil {
		client.write(wsOut{Type: "error", Message: "restart agent: " + runErr.Error()})
		_ = conn.Close()
		return
	}

	// Send the in-progress snapshot BEFORE subscribing so a late broadcast
	// cannot interleave ahead of it; the client rebuilds its UI from it.
	client.write(run.snapshot())
	run.subscribe(client)
	defer func() {
		run.unsubscribe(client)
		_ = conn.Close()
	}()

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
				client.ping()
			}
		}
	}()

	for {
		var in wsIn
		if err := conn.ReadJSON(&in); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		switch strings.ToLower(in.Type) {
		case "prompt":
			run.prompt(in.Text)
		case "cancel":
			run.cancelTurn()
		case "auto":
			run.setAuto(in.Enabled)
		case "permission":
			run.answerPermission(in.RequestID, in.OptionID)
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
