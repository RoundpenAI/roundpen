// Package agentapi serves Agent Web UI REST + SSE stream endpoints.
package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/browsetask"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
	"github.com/RoundpenAI/roundpen/internal/webpush"
)

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

	// Push delivers Web Push notifications for turn_done / permission events.
	Push webpush.Notifier

	// PublicURL is retained for callers that still pass the console base URL
	// (preview / terminal origin checks live elsewhere).
	PublicURL string

	// AutoMode classifies permission requests while the chat Auto toggle is on.
	AutoMode automode.Evaluator

	// ProxyURL resolves a user's agent-slot egress proxy ("" = direct).
	ProxyURL func(user *storage.User) string

	// LLMEnv resolves the relay endpoints and model pins for a user's agent
	// sandbox (nil = inject no model configuration).
	LLMEnv func(userID string) agentenv.LLMEnv

	// UserVirtualKey resolves the per-user llmgw virtual key injected into
	// newly provisioned agent sandboxes.
	UserVirtualKey func(ctx context.Context, username string) string

	// Profiles resolves coordinator / code-dev worker personas when restarting
	// an existing session (nil = default System Agent surface).
	Profiles SessionProfiler

	runnersMu sync.RWMutex
	runners   map[string]*runner
}

// SessionProfiler maps a persisted agent session to an ACP Start profile.
type SessionProfiler interface {
	ProfileFor(ctx context.Context, userID string, sess *agentsession.Session) string
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
	mux.HandleFunc("GET /v1/agent-sessions/{id}/commands", h.listCommands)
	mux.HandleFunc("GET /v1/agent-sessions/{id}/events", h.sessionEvents)
	mux.HandleFunc("POST /v1/agent-sessions/{id}/actions", h.sessionAction)
	h.mountBrowser(mux)
	h.mountTasks(mux)
}

func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"agents": h.ACP.Providers()})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	list, err := h.Store.ListByUser(r.Context(), user.Username, 50)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	if list == nil {
		list = []*agentsession.Session{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

type createReq struct {
	Title       string `json:"title"`
	ProviderID  string `json:"providerId"`
	AssistantID string `json:"assistantId"`
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
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

	sess, err := h.startSession(r.Context(), user, req.Title, req.ProviderID, req.AssistantID, "")
	if err != nil {
		writeSessionStartErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, sess)
}

type sessionStartError struct {
	Code int
	Msg  string
}

func (e *sessionStartError) Error() string { return e.Msg }

func writeSessionStartErr(w http.ResponseWriter, err error) {
	var se *sessionStartError
	if errors.As(err, &se) {
		httpx.WriteErr(w, se.Code, se.Msg)
		return
	}
	httpx.WriteErrOrInternal(w, nil, err, nil)
}

// DefaultAssistantProvider is the ACP provider used for new assistant chats.
// sysadmin runs in-process (no sandbox). Swap back to "claude" when QEMU coding agents are the default again.
const DefaultAssistantProvider = "sysadmin"

// StartForAssistant creates or is used by assistants ensure-session.
func (h *Handler) StartForAssistant(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error) {
	if title == "" {
		title = "Chat"
	}
	return h.startSession(ctx, user, title, DefaultAssistantProvider, assistantID, "")
}

// StartCoordinator opens a chat session with the project-coordinator persona
// (plan/delegate tools, no Write/Edit).
func (h *Handler) StartCoordinator(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error) {
	if title == "" {
		title = "Project"
	}
	return h.startSession(ctx, user, title, DefaultAssistantProvider, assistantID, "coordinator")
}

// EnsureCoordinatorRuntime starts the coordinator persona on an existing chat
// session when the in-process runtime is missing or was started without that
// profile (e.g. after process restart or a bare stream reconnect).
func (h *Handler) EnsureCoordinatorRuntime(ctx context.Context, user *storage.User, sessionID string) error {
	if rt, ok := h.ACP.Get(sessionID); ok && rt.Profile == "coordinator" {
		return nil
	}
	if _, ok := h.ACP.Get(sessionID); ok {
		h.deleteRunner(sessionID)
		h.ACP.Stop(sessionID)
	}
	assistantID := ""
	if sess, err := h.Store.Get(ctx, sessionID); err == nil && sess != nil {
		assistantID = sess.AssistantID
	}
	_, err := h.ACP.Start(ctx, sessionID, "", DefaultAssistantProvider, manager.StartOpts{
		Actor: manager.Actor{
			Username: user.Username,
			Role:     string(user.Role),
			APIKey:   user.APIKey,
		},
		AssistantID: assistantID,
		Project:     manager.ProjectStart{Profile: "coordinator"},
	})
	return err
}

// StartRoutine opens a kind=routine session and starts its System Agent with the
// narrowed tool surface for this firing.
func (h *Handler) StartRoutine(ctx context.Context, user *storage.User, assistantID, title, runKey, autonomy string, tokenBudget int) (string, error) {
	if title == "" {
		title = runKey
	}
	sess, err := h.Store.CreateKind(ctx, user.Username, title, DefaultAssistantProvider, "", assistantID, "routine")
	if err != nil {
		return "", err
	}
	if err := h.startRoutineRuntime(ctx, user, sess.ID, runKey, autonomy, tokenBudget); err != nil {
		return "", err
	}
	return sess.ID, nil
}

// CreateWorkerSession inserts a kind=worker session without starting the runtime.
// Used by the project delegate queue so queued workers have a session id to open.
func (h *Handler) CreateWorkerSession(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error) {
	if title == "" {
		title = "Worker"
	}
	return h.Store.CreateKind(ctx, user.Username, title, DefaultAssistantProvider, "", assistantID, "worker")
}

// StartWorkerRuntime starts (or restarts) the System Agent for a worker session
// with a code-dev role profile (implementer|reviewer|tester).
func (h *Handler) StartWorkerRuntime(ctx context.Context, user *storage.User, sessionID, role string) error {
	profile := role
	switch profile {
	case "implementer", "reviewer", "tester":
	default:
		profile = "implementer"
	}
	if rt, ok := h.ACP.Get(sessionID); ok && rt.Profile == profile {
		return nil
	}
	if _, ok := h.ACP.Get(sessionID); ok {
		h.deleteRunner(sessionID)
		h.ACP.Stop(sessionID)
	}
	assistantID := ""
	if sess, err := h.Store.Get(ctx, sessionID); err == nil && sess != nil {
		assistantID = sess.AssistantID
	}
	_, err := h.ACP.Start(ctx, sessionID, "", DefaultAssistantProvider, manager.StartOpts{
		Actor: manager.Actor{
			Username: user.Username,
			Role:     string(user.Role),
			APIKey:   user.APIKey,
		},
		AssistantID: assistantID,
		Project:     manager.ProjectStart{Profile: profile},
	})
	return err
}

func (h *Handler) profileFor(ctx context.Context, user *storage.User, sess *agentsession.Session) string {
	if h.Profiles == nil || user == nil || sess == nil {
		return ""
	}
	return h.Profiles.ProfileFor(ctx, user.Username, sess)
}

// ResumeRoutine starts the runtime again for an existing run session.
// When a continue segment reuses the Roundpen session, the live System Agent is
// rebound to the new run key so FinishRun targets the right row without tearing
// down the ACP session.
func (h *Handler) ResumeRoutine(ctx context.Context, user *storage.User, sessionID, runKey, autonomy string, tokenBudget int) error {
	if rt, ok := h.ACP.Get(sessionID); ok {
		if rt.CanRebindRoutine() {
			rt.RebindRoutine(runKey, autonomy, tokenBudget)
			return nil
		}
		h.deleteRunner(sessionID)
		h.ACP.Stop(sessionID)
	}
	return h.startRoutineRuntime(ctx, user, sessionID, runKey, autonomy, tokenBudget)
}

func (h *Handler) startRoutineRuntime(ctx context.Context, user *storage.User, sessionID, runKey, autonomy string, tokenBudget int) error {
	assistantID := ""
	if sess, err := h.Store.Get(ctx, sessionID); err == nil && sess != nil {
		assistantID = sess.AssistantID
	}
	_, err := h.ACP.Start(ctx, sessionID, "", DefaultAssistantProvider, manager.StartOpts{
		Actor: manager.Actor{
			Username: user.Username,
			Role:     string(user.Role),
			APIKey:   user.APIKey,
		},
		AssistantID: assistantID,
		Routine: manager.RoutineStart{
			RunKey:      runKey,
			Autonomy:    autonomy,
			TokenBudget: tokenBudget,
		},
	})
	return err
}

func (h *Handler) startSession(ctx context.Context, user *storage.User, title, providerID, assistantID, profile string) (*agentsession.Session, error) {
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
		vkey := ""
		if h.UserVirtualKey != nil {
			vkey = h.UserVirtualKey(ctx, user.Username)
		}
		prov := *h.Provisioner
		prov.Config.APIKey = user.APIKey
		prov.Config.VirtualKey = vkey
		prov.Config.ModelSource = user.ModelSource
		if h.ProxyURL != nil {
			prov.Config.ProxyURL = h.ProxyURL(user)
		}
		if provMeta.TemplateID != "" {
			prov.Config.TemplateID = provMeta.TemplateID
		}
		if h.LLMGW != nil {
			if h.LLMEnv != nil {
				prov.Config.LLMEnv = h.LLMEnv(user.Username)
			}
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
		Actor:       actor,
		AssistantID: sess.AssistantID,
		Project:     manager.ProjectStart{Profile: profile},
	}); err != nil {
		return nil, &sessionStartError{Code: http.StatusBadGateway, Msg: "start agent: " + err.Error()}
	}
	return sess, nil
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
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
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	if sess.UserID != user.Username && user.Role != "admin" {
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sess)
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	sess, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, agentsession.ErrNotFound) {
		httpx.WriteErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	if sess.UserID != user.Username && user.Role != "admin" {
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
		return
	}
	h.deleteRunner(id)
	if h.ACP != nil {
		h.ACP.Stop(id)
	}
	if err := h.Store.Delete(r.Context(), id); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
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
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	sess, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, agentsession.ErrNotFound) {
		httpx.WriteErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	if sess.UserID != user.Username && user.Role != "admin" {
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "title required")
		return
	}
	if err := h.Store.Rename(r.Context(), id, req.Title); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	sess.Title = req.Title
	httpx.WriteJSON(w, http.StatusOK, sess)
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) {
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
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			httpx.WriteErr(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		limit = n
	}
	before := strings.TrimSpace(r.URL.Query().Get("before"))
	msgs, hasMore, err := h.Store.ListMessagesPage(r.Context(), sess.ID, limit, before)
	if errors.Is(err, agentsession.ErrNotFound) {
		httpx.WriteErr(w, http.StatusBadRequest, "before message not found")
		return
	}
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	if msgs == nil {
		msgs = []*agentsession.Message{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"messages": msgs,
		"hasMore":  hasMore,
	})
}
