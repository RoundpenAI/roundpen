package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// ProjectBinder resolves a chat session to an owning project id (optional).
type ProjectBinder interface {
	ProjectIDForSession(ctx context.Context, userID, sessionID string) (string, error)
}

// WorkPulser wakes agents to advance an issue (work pulse). Reason is opaque
// to this package (created|transition|tick|worker_done|task_resume).
type WorkPulser interface {
	EnqueueIssue(ctx context.Context, userID, issueKey, reason string)
}

// TaskSessionContinuer appends a follow-up turn to a task-linked agent session
// so a human unblock actually resumes work (status-only patches leave the
// transcript idle).
type TaskSessionContinuer interface {
	ContinueTaskSession(ctx context.Context, user *storage.User, sessionID, prompt string) error
}

// Handler serves the issue tracker: /v1/issues, their docs and tasks.
type Handler struct {
	Store     *Store
	Projects  ProjectBinder
	Pulse     WorkPulser
	Continues TaskSessionContinuer
}

// Mount registers the issue tracker routes on the console API mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/issues", h.listIssues)
	mux.HandleFunc("POST /v1/issues", h.createIssue)
	mux.HandleFunc("GET /v1/issues/board", h.board)
	mux.HandleFunc("GET /v1/issues/{key}", h.getIssue)
	mux.HandleFunc("PATCH /v1/issues/{key}", h.patchIssue)
	mux.HandleFunc("POST /v1/issues/{key}/transitions", h.transitionIssue)
	mux.HandleFunc("GET /v1/issues/{key}/docs", h.listDocs)
	mux.HandleFunc("POST /v1/issues/{key}/docs", h.createDoc)
	mux.HandleFunc("GET /v1/issues/{key}/docs/{docKey}", h.getDoc)
	mux.HandleFunc("GET /v1/issues/{key}/tasks", h.listTasks)
	mux.HandleFunc("POST /v1/issues/{key}/tasks", h.createTask)
	mux.HandleFunc("PATCH /v1/tasks/{key}", h.patchTask)
}

func (h *Handler) listIssues(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var f ListFilter
	if status := r.URL.Query().Get("status"); status != "" {
		if err := ValidateIssueStatus(status); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
		f.Status = status
	}
	if projectID := r.URL.Query().Get("projectId"); projectID != "" {
		f.ProjectID = projectID
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		f.Limit = n
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	items, err := h.Store.List(r.Context(), user.Username, f)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"issues": items})
}

func (h *Handler) createIssue(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Title     string `json:"title"`
		Summary   string `json:"summary"`
		SessionID string `json:"sessionId"` // 助手工具传入；控制台不传
		ProjectID string `json:"projectId"` // optional; auto from coordinator session when omitted
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := ValidateTitle(body.Title); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	// The client never supplies assistantId: it is always derived from the
	// session row, so the recorded origin cannot be forged.
	in := CreateInput{
		Title: body.Title, Summary: body.Summary, Origin: OriginConsole,
		ProjectID: strings.TrimSpace(body.ProjectID),
	}
	if body.SessionID != "" {
		assistantID, ok := h.resolveSession(w, r, user.Username, body.SessionID)
		if !ok {
			return
		}
		in.SessionID = body.SessionID
		in.AssistantID = assistantID
		in.Origin = OriginChat
		if in.ProjectID == "" && h.Projects != nil {
			if pid, err := h.Projects.ProjectIDForSession(r.Context(), user.Username, body.SessionID); err == nil {
				in.ProjectID = pid
			}
		}
	}
	it, err := h.Store.Create(r.Context(), user.Username, in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if h.Pulse != nil {
		h.Pulse.EnqueueIssue(r.Context(), user.Username, it.Key, "created")
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"issue": it})
}

func (h *Handler) getIssue(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	it, docs, tasks, err := h.Store.Get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"issue": it, "docs": docs, "tasks": tasks})
}

func (h *Handler) patchIssue(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Status  *string `json:"status"`
		Title   *string `json:"title"`
		Summary *string `json:"summary"`
		PRURL   *string `json:"prUrl"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Status != nil {
		if err := ValidateIssueStatus(*body.Status); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.Title != nil {
		if err := ValidateTitle(*body.Title); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	it, err := h.Store.Update(r.Context(), user.Username, r.PathValue("key"), UpdateInput{
		Status:  body.Status,
		Title:   body.Title,
		Summary: body.Summary,
		PRURL:   body.PRURL,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"issue": it})
}

func (h *Handler) transitionIssue(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		To          string `json:"to"`
		Reason      string `json:"reason"`
		Evidence    string `json:"evidence"`
		ActorKind   string `json:"actorKind"`
		ActorID     string `json:"actorId"`
		AssistantID string `json:"assistantId"`
		Force       bool   `json:"force"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	to := strings.TrimSpace(body.To)
	if to == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "to is required")
		return
	}
	if err := ValidateIssueStatus(to); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	kind := strings.TrimSpace(body.ActorKind)
	if kind == "" {
		kind = ActorHuman
	}
	actorID := strings.TrimSpace(body.ActorID)
	if actorID == "" {
		actorID = strings.TrimSpace(body.AssistantID)
	}
	if actorID == "" && kind == ActorHuman {
		actorID = user.Username
	}
	req := TransitionRequest{
		To: to, Reason: body.Reason, Evidence: body.Evidence,
		ActorKind: kind, ActorID: actorID, Force: body.Force,
	}
	it, err := h.Store.TransitionWith(r.Context(), user.Username, r.PathValue("key"), req)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if h.Pulse != nil && !IsClosedStatus(it.Status) {
		h.Pulse.EnqueueIssue(r.Context(), user.Username, it.Key, "transition")
	}
	out := map[string]any{"issue": it}
	if reason := strings.TrimSpace(body.Reason); reason != "" {
		out["reason"] = reason
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	var f ListFilter
	if p := r.URL.Query().Get("projectId"); p != "" {
		f.ProjectID = p
	}
	if lim := r.URL.Query().Get("limit"); lim != "" {
		n, err := strconv.Atoi(lim)
		if err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		f.Limit = n
	}
	if f.Limit <= 0 {
		f.Limit = 200
	}
	items, err := h.Store.List(r.Context(), user.Username, f)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	columns := make([]map[string]any, 0, len(BoardColumnOrder))
	byStatus := map[string][]Issue{}
	for _, it := range items {
		byStatus[it.Status] = append(byStatus[it.Status], it)
	}
	for _, st := range BoardColumnOrder {
		cols := byStatus[st]
		if cols == nil {
			cols = []Issue{}
		}
		columns = append(columns, map[string]any{
			"status":   st,
			"terminal": IsClosedStatus(st),
			"issues":   cols,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"columns":  columns,
		"order":    BoardColumnOrder,
		"terminal": TerminalStatuses,
	})
}

func (h *Handler) listDocs(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "" {
		if err := ValidateKind(kind); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	docs, err := h.Store.ListDocs(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if kind != "" { // ListDocs has no kind filter; the list is small and per-issue.
		filtered := make([]Doc, 0, len(docs))
		for _, d := range docs {
			if d.Kind == kind {
				filtered = append(filtered, d)
			}
		}
		docs = filtered
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"docs": docs})
}

func (h *Handler) createDoc(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Kind       string `json:"kind"`
		Title      string `json:"title"`
		ContentMD  string `json:"contentMd"`
		Status     string `json:"status"`
		TaskKey    string `json:"taskKey"`
		AuthorType string `json:"authorType"`
		SessionID  string `json:"sessionId"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := ValidateKind(body.Kind); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.ContentMD) == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "contentMd is required")
		return
	}
	if body.Status != "" {
		if err := ValidateDocStatus(body.Status); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	authorType := body.AuthorType
	if authorType == "" {
		authorType = "assistant"
	}
	if authorType != "user" && authorType != "assistant" {
		httpx.WriteErr(w, http.StatusBadRequest, "authorType must be user or assistant")
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	in := DocInput{
		Kind: body.Kind, Title: body.Title, ContentMD: body.ContentMD, Status: body.Status,
		TaskKey: body.TaskKey, AuthorType: authorType,
	}
	if body.SessionID != "" {
		assistantID, ok := h.resolveSession(w, r, user.Username, body.SessionID)
		if !ok {
			return
		}
		in.SessionID = body.SessionID
		in.AssistantID = assistantID
	}
	doc, err := h.Store.WriteDoc(r.Context(), user.Username, r.PathValue("key"), in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"doc": doc})
}

func (h *Handler) getDoc(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	// docKey is a short key, a uuid or "latest" — GetDoc resolves all three and
	// scopes the read through the issue's owner.
	doc, err := h.Store.GetDoc(r.Context(), user.Username, r.PathValue("key"), r.PathValue("docKey"), 0)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"doc": doc})
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	tasks, err := h.Store.ListTasks(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Title      string `json:"title"`
		Detail     string `json:"detail"`
		Position   int    `json:"position"`
		PlanDocKey string `json:"planDocKey"`
		Status     string `json:"status"`
		SessionID  string `json:"sessionId"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := ValidateTitle(body.Title); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Status != "" {
		if err := ValidateTaskStatus(body.Status); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	in := TaskInput{
		Title: body.Title, Detail: body.Detail, Position: body.Position,
		Status: body.Status, PlanDocKey: body.PlanDocKey,
	}
	if body.SessionID != "" {
		assistantID, ok := h.resolveSession(w, r, user.Username, body.SessionID)
		if !ok {
			return
		}
		in.SessionID = body.SessionID
		in.AssistantID = assistantID
	}
	task, err := h.Store.CreateTask(r.Context(), user.Username, r.PathValue("key"), in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"task": task})
}

func (h *Handler) patchTask(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Status    *string `json:"status"`
		Title     *string `json:"title"`
		Detail    *string `json:"detail"`
		SessionID *string `json:"sessionId"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Status != nil {
		if err := ValidateTaskStatus(*body.Status); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.Title != nil {
		if err := ValidateTitle(*body.Title); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	in := TaskUpdateInput{Status: body.Status, Title: body.Title, Detail: body.Detail}
	if body.SessionID != nil && *body.SessionID != "" {
		if _, ok := h.resolveSession(w, r, user.Username, *body.SessionID); !ok {
			return
		}
		in.SessionID = body.SessionID
	}
	keyOrID := r.PathValue("key")
	var prevStatus string
	if body.Status != nil {
		if prev, err := h.Store.GetTask(r.Context(), user.Username, keyOrID); err == nil && prev != nil {
			prevStatus = prev.Status
		}
	}
	task, err := h.Store.UpdateTask(r.Context(), user.Username, keyOrID, in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	h.afterTaskUnblocked(r.Context(), user, prevStatus, task)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"task": task})
}

// afterTaskUnblocked resumes agent work when a human clears a blocked TSK.
// A status-only patch left the linked worker session idle — append a turn and
// pulse the coordinator so the queue can re-delegate if needed.
func (h *Handler) afterTaskUnblocked(ctx context.Context, user *storage.User, prevStatus string, task *Task) {
	if h == nil || user == nil || task == nil {
		return
	}
	if prevStatus != TaskBlocked || task.Status != TaskInProgress {
		return
	}
	issueKey := ""
	if h.Store != nil {
		if k, err := h.Store.IssueKeyForID(ctx, user.Username, task.IssueID); err == nil {
			issueKey = k
		}
	}
	if h.Pulse != nil && issueKey != "" {
		h.Pulse.EnqueueIssue(ctx, user.Username, issueKey, "task_resume")
	}
	sid := strings.TrimSpace(task.SessionID)
	if sid == "" || h.Continues == nil {
		return
	}
	prompt := fmt.Sprintf(
		"Human cleared the blocker and marked %s in_progress again.\n"+
			"Title: %s\n"+
			"Prior detail / blocker notes (may be stale — retry tools):\n%s\n\n"+
			"Continue this task now. Re-try Bash / browser / build / test as the brief requires. "+
			"UpdateTask with progress or a new blocker; do not stop solely because an earlier turn saw Permission rejected.",
		task.Key, strings.TrimSpace(task.Title), strings.TrimSpace(task.Detail),
	)
	_ = h.Continues.ContinueTaskSession(ctx, user, sid, prompt)
}

// resolveSession checks that a client-supplied sessionId belongs to the caller
// and returns the assistant bound to it. A missing or foreign session is a
// client mistake (400) rather than 403/404: the response must not confirm that
// someone else's session id exists.
func (h *Handler) resolveSession(w http.ResponseWriter, r *http.Request, userID, sessionID string) (string, bool) {
	assistantID, err := h.Store.SessionAssistant(r.Context(), userID, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.WriteErr(w, http.StatusBadRequest, "sessionId does not belong to the caller")
			return "", false
		}
		writeStoreErr(w, err)
		return "", false
	}
	return assistantID, true
}

// writeStoreErr maps store failures onto the API contract. Callers validate
// request fields before touching the store, so anything but a missing row is an
// internal failure — a failed write must never masquerade as a 400.
func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		httpx.WriteErr(w, http.StatusNotFound, "not found")
		return
	}
	var inv ErrInvalidTransition
	if errors.As(err, &inv) {
		httpx.WriteErr(w, http.StatusBadRequest, inv.Error())
		return
	}
	var thrash ErrThrash
	if errors.As(err, &thrash) {
		httpx.WriteErr(w, http.StatusConflict, thrash.Error())
		return
	}
	httpx.WriteErrOrInternal(w, nil, err, nil)
}

// decodeBody reads a size-capped JSON body into v. An empty body decodes to the
// zero struct so PATCH may send only the fields it changes.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}
