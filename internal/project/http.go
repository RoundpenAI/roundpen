package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/gitcred"
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
	StartCoordinator(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error)
	EnsureCoordinatorRuntime(ctx context.Context, user *storage.User, sessionID string) error
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

// GitCreds looks up stored host tokens for optional CI status refresh.
type GitCreds interface {
	List(ctx context.Context, userID string) ([]gitcred.Cred, error)
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
	GitCreds   GitCreds
	HTTP       *http.Client // optional; used for GitHub Checks API
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
	mux.HandleFunc("POST /v1/projects/{key}/workers/{workerKey}/decision", h.workerDecision)
	mux.HandleFunc("GET /v1/projects/{key}/subscriptions", h.listSubscriptions)
	mux.HandleFunc("POST /v1/projects/{key}/subscriptions", h.createSubscription)
	mux.HandleFunc("PATCH /v1/projects/{key}/subscriptions/{subKey}", h.patchSubscription)
	mux.HandleFunc("DELETE /v1/projects/{key}/subscriptions/{subKey}", h.deleteSubscription)
	mux.HandleFunc("GET /v1/projects/{key}/ci-status", h.ciStatus)
	mux.HandleFunc("POST /v1/hooks/github/{subKey}", h.githubWebhook)
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

	if h.Starter == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "session starter unavailable")
		return
	}
	// Reuse the newest chat session when present; ensure coordinator persona runtime.
	if h.Sessions != nil {
		list, err := h.Sessions.ListByAssistant(r.Context(), user.Username, p.CoordinatorAssistantID, 1)
		if err != nil {
			httpx.WriteErrOrInternal(w, r, err, nil)
			return
		}
		if len(list) > 0 {
			existing := list[0]
			if meta, ok := providers.ByID(providers.Default(), existing.ProviderID); ok && !providers.NeedsSandbox(meta) {
				if err := h.Starter.EnsureCoordinatorRuntime(r.Context(), user, existing.ID); err != nil {
					httpx.WriteErrOrInternal(w, r, err, nil)
					return
				}
				p, _ = h.Store.SetCoordinatorSession(r.Context(), user.Username, p.Key, existing.ID)
				httpx.WriteJSON(w, http.StatusOK, map[string]any{
					"sessionId": existing.ID,
					"project":   p,
				})
				return
			}
		}
	}
	sess, err := h.Starter.StartCoordinator(r.Context(), user, p.CoordinatorAssistantID, p.Name)
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
		Role     string `json:"role"`
		IssueKey string `json:"issueKey"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := ValidateWorkerTitle(body.Title); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	role, err := NormalizeWorkerRole(body.Role)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in := DelegateInput{Title: body.Title, Brief: body.Brief, Role: role}
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

// workerDecision records Pass / Request changes for a finished reviewer worker.
// Request changes queues a follow-up implementer with the review findings as brief.
func (h *Handler) workerDecision(w http.ResponseWriter, r *http.Request) {
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
	if wk.Role != RoleReviewer {
		httpx.WriteErr(w, http.StatusBadRequest, "only reviewer workers accept Pass / Request changes")
		return
	}
	if wk.Status != WorkerSucceeded && wk.Status != WorkerFailed {
		httpx.WriteErr(w, http.StatusConflict, "worker has not finished")
		return
	}
	var body struct {
		Decision string `json:"decision"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	decision := strings.TrimSpace(strings.ToLower(body.Decision))
	if decision != DecisionPassed && decision != DecisionChangesRequested {
		httpx.WriteErr(w, http.StatusBadRequest, "decision must be passed or changes_requested")
		return
	}
	if err := h.Store.SetWorkerUserDecision(r.Context(), wk.ID, decision); err != nil {
		httpx.WriteErr(w, http.StatusConflict, err.Error())
		return
	}
	wk.UserDecision = decision

	var followUp *Worker
	if decision == DecisionChangesRequested {
		if p.Status != StatusActive {
			httpx.WriteErr(w, http.StatusConflict, "project is archived")
			return
		}
		if h.Workers == nil || p.CoordinatorAssistantID == "" {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "cannot queue follow-up implementer")
			return
		}
		title := "Address review: " + wk.Title
		if len([]rune(title)) > 200 {
			title = string([]rune(title)[:200])
		}
		brief := FormatChangesBrief(wk.Review, wk.Title)
		sess, err := h.Workers.CreateWorkerSession(r.Context(), user, p.CoordinatorAssistantID, title)
		if err != nil {
			httpx.WriteErrOrInternal(w, r, err, nil)
			return
		}
		followUp, err = h.Store.CreateWorker(r.Context(), user.Username, p.ID, DelegateInput{
			Title:   title,
			Brief:   brief,
			Role:    RoleImplementer,
			IssueID: wk.IssueID,
		}, sess.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if h.Runner != nil {
			h.Runner.Kick(r.Context(), user.Username)
			if fresh, err := h.Store.GetWorker(r.Context(), user.Username, followUp.Key); err == nil {
				followUp = fresh
			}
		}
	}

	out := map[string]any{"worker": wk}
	if followUp != nil {
		out["followUp"] = followUp
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
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
	items, err := h.Store.ListSubscriptions(r.Context(), user.Username, p.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"subscriptions": items})
}

func (h *Handler) createSubscription(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		Kind string `json:"kind"`
		Repo string `json:"repo"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	repo := body.Repo
	if strings.TrimSpace(repo) == "" {
		repo = p.RepoURL
	}
	sub, err := h.Store.CreateSubscription(r.Context(), user.Username, p.ID, body.Kind, repo, "")
	if err != nil {
		if errors.Is(err, ErrSubKindTaken) {
			httpx.WriteErr(w, http.StatusConflict, err.Error())
			return
		}
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"subscription": sub})
}

func (h *Handler) patchSubscription(w http.ResponseWriter, r *http.Request) {
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
	sub, err := h.Store.GetSubscription(r.Context(), user.Username, r.PathValue("subKey"))
	if err != nil {
		if errors.Is(err, ErrSubNotFound) {
			httpx.WriteErr(w, http.StatusNotFound, "subscription not found")
			return
		}
		writeStoreErr(w, err)
		return
	}
	if sub.ProjectID != p.ID {
		httpx.WriteErr(w, http.StatusNotFound, "subscription not found")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Enabled == nil {
		httpx.WriteErr(w, http.StatusBadRequest, "enabled is required")
		return
	}
	sub, err = h.Store.SetSubscriptionEnabled(r.Context(), user.Username, sub.Key, *body.Enabled)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"subscription": sub})
}

func (h *Handler) deleteSubscription(w http.ResponseWriter, r *http.Request) {
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
	sub, err := h.Store.GetSubscription(r.Context(), user.Username, r.PathValue("subKey"))
	if err != nil {
		if errors.Is(err, ErrSubNotFound) {
			httpx.WriteErr(w, http.StatusNotFound, "subscription not found")
			return
		}
		writeStoreErr(w, err)
		return
	}
	if sub.ProjectID != p.ID {
		httpx.WriteErr(w, http.StatusNotFound, "subscription not found")
		return
	}
	if err := h.Store.DeleteSubscription(r.Context(), user.Username, sub.Key); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ciStatus returns the cached CI badge from the github_ci subscription, optionally
// refreshing from the GitHub Checks API when a stored github.com token exists.
func (h *Handler) ciStatus(w http.ResponseWriter, r *http.Request) {
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
	subs, err := h.Store.ListSubscriptions(r.Context(), user.Username, p.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var ci *Subscription
	for _, s := range subs {
		if s.Kind == SubKindGitHubCI {
			ci = s
			break
		}
	}
	out := map[string]any{
		"status":  "unknown",
		"summary": "",
		"source":  "none",
		"repo":    "",
	}
	if ci != nil {
		out["status"] = ci.LastStatus
		if ci.LastStatus == "" {
			out["status"] = "unknown"
		}
		out["summary"] = ci.LastSummary
		out["source"] = "webhook"
		out["repo"] = ci.Repo
		out["subscriptionKey"] = ci.Key
		if ci.LastEventAt != nil {
			out["lastEventAt"] = ci.LastEventAt
		}
	}
	refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	repo := ""
	if ci != nil {
		repo = ci.Repo
	}
	if repo == "" {
		if n, err := NormalizeRepo(p.RepoURL); err == nil {
			repo = n
			out["repo"] = repo
		}
	}
	if refresh && repo != "" && h.GitCreds != nil {
		if status, summary, err := h.fetchGitHubCI(r.Context(), user.Username, repo); err == nil && status != "" {
			out["status"] = status
			out["summary"] = summary
			out["source"] = "api"
			if ci != nil {
				_ = h.Store.TouchSubscriptionEvent(r.Context(), ci.ID, status, summary)
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) fetchGitHubCI(ctx context.Context, userID, repo string) (status, summary string, err error) {
	creds, err := h.GitCreds.List(ctx, userID)
	if err != nil {
		return "", "", err
	}
	var token string
	for _, c := range creds {
		if c.Provider == gitcred.ProviderGitHub && strings.TrimSpace(c.Token) != "" {
			token = c.Token
			break
		}
		if c.Host == "github.com" && strings.TrimSpace(c.Token) != "" {
			token = c.Token
			break
		}
	}
	if token == "" {
		return "", "", errors.New("no github token")
	}
	client := h.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return "", "", errors.New("invalid repo")
	}
	u := "https://api.github.com/repos/" + parts[0] + "/" + parts[1] + "/commits/HEAD/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("github status %d", resp.StatusCode)
	}
	var payload struct {
		State string `json:"state"`
		URL   string `json:"html_url"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", "", err
	}
	st := mapLegacyStatus(payload.State)
	sum := "Combined status: " + payload.State
	if payload.URL != "" {
		sum += "\n" + payload.URL
	}
	return st, sum, nil
}

func (h *Handler) githubWebhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	subKey := r.PathValue("subKey")
	sub, err := h.Store.GetSubscriptionByKey(r.Context(), subKey)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "subscription not found")
		return
	}
	if !sub.Enabled {
		httpx.WriteErr(w, http.StatusConflict, "subscription disabled")
		return
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if !VerifyGitHubSignature(sub.Secret, raw, sig) {
		httpx.WriteErr(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "ping" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "pong": true})
		return
	}
	if !EventMatchesKind(sub.Kind, eventType) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": true})
		return
	}
	ev := ParseGitHubWebhook(eventType, raw)
	if sub.Repo != "" && ev.Repo != "" && !strings.EqualFold(sub.Repo, ev.Repo) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": "repo mismatch"})
		return
	}
	_ = h.Store.TouchSubscriptionEvent(r.Context(), sub.ID, ev.Status, ev.Summary)

	// Load project for coordinator notify (Get accepts id).
	p, err := h.Store.Get(r.Context(), sub.UserID, sub.ProjectID)
	if err != nil {
		h.log().Warn("webhook project lookup", "sub", sub.Key, "err", err)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "notified": false})
		return
	}
	if h.Runner != nil {
		repo := ev.Repo
		if repo == "" {
			repo = sub.Repo
		}
		h.Runner.NotifyGitHubEvent(r.Context(), p, sub.Kind, repo, ev.Summary)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "notified": true})
}

func (h *Handler) log() *slog.Logger {
	if h != nil && h.Log != nil {
		return h.Log
	}
	return slog.Default()
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
	case errors.Is(err, ErrSubNotFound):
		httpx.WriteErr(w, http.StatusNotFound, "subscription not found")
	case errors.Is(err, ErrSubKindTaken):
		httpx.WriteErr(w, http.StatusConflict, err.Error())
	case err != nil && (strings.Contains(err.Error(), "required") ||
		strings.Contains(err.Error(), "must ") ||
		strings.Contains(err.Error(), "invalid") ||
		strings.Contains(err.Error(), "too long") ||
		strings.Contains(err.Error(), "match") ||
		strings.Contains(err.Error(), "look like")):
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
	default:
		httpx.WriteErr(w, http.StatusInternalServerError, "internal error")
	}
}
