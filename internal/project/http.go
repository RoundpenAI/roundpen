package project

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Assistants creates and updates coordinator assistants.
type Assistants interface {
	Create(ctx context.Context, userID string, in assistant.CreateInput) (*assistant.Assistant, error)
	Update(ctx context.Context, id string, in assistant.UpdateInput) (*assistant.Assistant, error)
	Get(ctx context.Context, id string) (*assistant.Assistant, error)
}

// SessionStarter starts an ACP session bound to the coordinator assistant.
type SessionStarter interface {
	StartForAssistant(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error)
}

// WorkerSessions creates kind=worker sessions for the delegate queue.
type WorkerSessions interface {
	CreateWorkerSession(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error)
}

// IssueResolver looks up an issue key owned by the user (for optional linking).
type IssueResolver interface {
	LookupID(ctx context.Context, userID, keyOrID string) (string, error)
}

// DirSeeder best-effort creates projects/<slug>/ and Shared Context on disk.
type DirSeeder interface {
	EnsureProjectTree(ctx context.Context, userID, slug string) error
}

// Handler serves /v1/projects.
type Handler struct {
	Store      *Store
	Assistants Assistants
	Sessions   *agentsession.Store
	Starter    SessionStarter
	Workers    WorkerSessions
	Issues     IssueResolver
	Runner     *Runner
	Dirs       DirSeeder
	Log        *slog.Logger
}

// Mount registers project routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/projects", h.list)
	mux.HandleFunc("POST /v1/projects", h.create)
	mux.HandleFunc("GET /v1/projects/{key}", h.get)
	mux.HandleFunc("PATCH /v1/projects/{key}", h.patch)
	mux.HandleFunc("POST /v1/projects/{key}/ensure-session", h.ensureSession)
	mux.HandleFunc("GET /v1/projects/{key}/workers", h.listWorkers)
	mux.HandleFunc("POST /v1/projects/{key}/workers", h.delegate)
	mux.HandleFunc("GET /v1/projects/{key}/workers/{workerKey}", h.getWorker)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var f ListFilter
	if status := r.URL.Query().Get("status"); status != "" {
		if err := ValidateStatus(status); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
		f.Status = status
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		f.Limit = n
	}
	items, err := h.Store.List(r.Context(), user.Username, f)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"projects": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Name    string `json:"name"`
		Icon    string `json:"icon"`
		Slug    string `json:"slug"`
		RepoURL string `json:"repoUrl"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	in := CreateInput{
		Name: body.Name, Icon: body.Icon, Slug: body.Slug, RepoURL: body.RepoURL,
	}
	if err := ValidateName(in.Name); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	slug, err := ResolveSlug(in.Slug, in.Name)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Slug = slug
	if err := ValidateRepoURL(in.RepoURL); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.Assistants == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "assistants unavailable")
		return
	}

	coord, err := h.Assistants.Create(r.Context(), user.Username, assistant.DefaultCoordinatorCreateInput(in.Name))
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}

	p, err := h.Store.Create(r.Context(), user.Username, in, coord.ID)
	if err != nil {
		// Best-effort rollback so a failed insert does not leave an orphan coordinator.
		disabled := assistant.StatusDisabled
		_, _ = h.Assistants.Update(r.Context(), coord.ID, assistant.UpdateInput{Status: &disabled})
		writeStoreErr(w, err)
		return
	}

	if h.Dirs != nil {
		if err := h.Dirs.EnsureProjectTree(r.Context(), user.Username, p.Slug); err != nil {
			if h.Log != nil {
				h.Log.Warn("seed project dirs", "slug", p.Slug, "err", err)
			}
		}
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"project": p})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, err := h.Store.Get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"project": p})
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Name    *string `json:"name"`
		Icon    *string `json:"icon"`
		RepoURL *string `json:"repoUrl"`
		Status  *string `json:"status"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	p, err := h.Store.Update(r.Context(), user.Username, r.PathValue("key"), UpdateInput{
		Name: body.Name, Icon: body.Icon, RepoURL: body.RepoURL, Status: body.Status,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if body.Status != nil && *body.Status == StatusArchived && p.CoordinatorAssistantID != "" && h.Assistants != nil {
		disabled := assistant.StatusDisabled
		_, _ = h.Assistants.Update(r.Context(), p.CoordinatorAssistantID, assistant.UpdateInput{Status: &disabled})
	}
	if body.Status != nil && *body.Status == StatusActive && p.CoordinatorAssistantID != "" && h.Assistants != nil {
		active := assistant.StatusActive
		_, _ = h.Assistants.Update(r.Context(), p.CoordinatorAssistantID, assistant.UpdateInput{Status: &active})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"project": p})
}

func (h *Handler) ensureSession(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, err := h.Store.Get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if p.CoordinatorAssistantID == "" {
		httpx.WriteErr(w, http.StatusConflict, "project has no coordinator")
		return
	}
	if p.Status != StatusActive {
		httpx.WriteErr(w, http.StatusConflict, "project is archived")
		return
	}

	// Reuse the newest in-process chat session when present (same idea as assistants).
	if h.Sessions != nil {
		list, err := h.Sessions.ListByAssistant(r.Context(), user.Username, p.CoordinatorAssistantID, 1)
		if err != nil {
			httpx.WriteErrOrInternal(w, r, err, nil)
			return
		}
		if len(list) > 0 {
			existing := list[0]
			if meta, ok := providers.ByID(providers.Default(), existing.ProviderID); ok && !providers.NeedsSandbox(meta) {
				p, _ = h.Store.SetCoordinatorSession(r.Context(), user.Username, p.Key, existing.ID)
				httpx.WriteJSON(w, http.StatusOK, map[string]any{
					"sessionId": existing.ID,
					"project":   p,
				})
				return
			}
		}
	}
	if h.Starter == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "session starter unavailable")
		return
	}
	sess, err := h.Starter.StartForAssistant(r.Context(), user, p.CoordinatorAssistantID, p.Name)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	p, _ = h.Store.SetCoordinatorSession(r.Context(), user.Username, p.Key, sess.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"sessionId": sess.ID,
		"project":   p,
	})
}

func (h *Handler) listWorkers(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, err := h.Store.Get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	items, err := h.Store.ListWorkers(r.Context(), user.Username, p.ID, 100)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"workers": items})
}

func (h *Handler) getWorker(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, err := h.Store.Get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	wk, err := h.Store.GetWorker(r.Context(), user.Username, r.PathValue("workerKey"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if wk.ProjectID != p.ID {
		httpx.WriteErr(w, http.StatusNotFound, "worker not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"worker": wk})
}

func (h *Handler) delegate(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, err := h.Store.Get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if p.Status != StatusActive {
		httpx.WriteErr(w, http.StatusConflict, "project is archived")
		return
	}
	if p.CoordinatorAssistantID == "" {
		httpx.WriteErr(w, http.StatusConflict, "project has no coordinator")
		return
	}
	var body struct {
		Title    string `json:"title"`
		Brief    string `json:"brief"`
		IssueKey string `json:"issueKey"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := ValidateWorkerTitle(body.Title); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in := DelegateInput{Title: body.Title, Brief: body.Brief}
	if body.IssueKey != "" {
		if h.Issues == nil {
			httpx.WriteErr(w, http.StatusBadRequest, "issue linking unavailable")
			return
		}
		issueID, err := h.Issues.LookupID(r.Context(), user.Username, body.IssueKey)
		if err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "issue not found")
			return
		}
		in.IssueID = issueID
	}
	if h.Workers == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "worker sessions unavailable")
		return
	}
	sess, err := h.Workers.CreateWorkerSession(r.Context(), user, p.CoordinatorAssistantID, body.Title)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	wk, err := h.Store.CreateWorker(r.Context(), user.Username, p.ID, in, sess.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if h.Runner != nil {
		h.Runner.Kick(r.Context(), user.Username)
		// Re-read so the response reflects queued→running when the slot was free.
		if fresh, err := h.Store.GetWorker(r.Context(), user.Username, wk.Key); err == nil {
			wk = fresh
		}
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"worker": wk})
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if len(raw) == 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteErr(w, http.StatusNotFound, "project not found")
	case errors.Is(err, ErrSlugTaken):
		httpx.WriteErr(w, http.StatusConflict, "project slug already exists")
	case err != nil && (strings.Contains(err.Error(), "required") ||
		strings.Contains(err.Error(), "must ") ||
		strings.Contains(err.Error(), "invalid") ||
		strings.Contains(err.Error(), "too long") ||
		strings.Contains(err.Error(), "match")):
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
	default:
		httpx.WriteErr(w, http.StatusInternalServerError, "internal error")
	}
}
