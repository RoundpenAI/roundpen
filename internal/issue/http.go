package issue

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
)

// Handler serves the issue tracker: /v1/issues, their docs and tasks.
type Handler struct {
	Store *Store
}

// Mount registers the issue tracker routes on the console API mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/issues", h.listIssues)
	mux.HandleFunc("POST /v1/issues", h.createIssue)
	mux.HandleFunc("GET /v1/issues/{key}", h.getIssue)
	mux.HandleFunc("PATCH /v1/issues/{key}", h.patchIssue)
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
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var f ListFilter
	if status := r.URL.Query().Get("status"); status != "" {
		if err := ValidateIssueStatus(status); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		f.Status = status
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		f.Limit = n
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	items, err := h.Store.List(r.Context(), user.Username, f)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": items})
}

func (h *Handler) createIssue(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Title     string `json:"title"`
		Summary   string `json:"summary"`
		SessionID string `json:"sessionId"` // 助手工具传入；控制台不传
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := ValidateTitle(body.Title); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	// The client never supplies assistantId: it is always derived from the
	// session row, so the recorded origin cannot be forged.
	in := CreateInput{Title: body.Title, Summary: body.Summary, Origin: OriginConsole}
	if body.SessionID != "" {
		assistantID, ok := h.resolveSession(w, r, user.Username, body.SessionID)
		if !ok {
			return
		}
		in.SessionID = body.SessionID
		in.AssistantID = assistantID
		in.Origin = OriginChat
	}
	it, err := h.Store.Create(r.Context(), user.Username, in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"issue": it})
}

func (h *Handler) getIssue(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	it, docs, tasks, err := h.Store.Get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issue": it, "docs": docs, "tasks": tasks})
}

func (h *Handler) patchIssue(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Status  *string `json:"status"`
		Title   *string `json:"title"`
		Summary *string `json:"summary"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Status != nil {
		if err := ValidateIssueStatus(*body.Status); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.Title != nil {
		if err := ValidateTitle(*body.Title); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	it, err := h.Store.Update(r.Context(), user.Username, r.PathValue("key"), UpdateInput{
		Status:  body.Status,
		Title:   body.Title,
		Summary: body.Summary,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issue": it})
}

func (h *Handler) listDocs(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "" {
		if err := ValidateKind(kind); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
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
	writeJSON(w, http.StatusOK, map[string]any{"docs": docs})
}

func (h *Handler) createDoc(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
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
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.ContentMD) == "" {
		writeErr(w, http.StatusBadRequest, "contentMd is required")
		return
	}
	if body.Status != "" {
		if err := ValidateDocStatus(body.Status); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	authorType := body.AuthorType
	if authorType == "" {
		authorType = "assistant"
	}
	if authorType != "user" && authorType != "assistant" {
		writeErr(w, http.StatusBadRequest, "authorType must be user or assistant")
		return
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
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
	writeJSON(w, http.StatusCreated, map[string]any{"doc": doc})
}

func (h *Handler) getDoc(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	// docKey is a short key, a uuid or "latest" — GetDoc resolves all three and
	// scopes the read through the issue's owner.
	doc, err := h.Store.GetDoc(r.Context(), user.Username, r.PathValue("key"), r.PathValue("docKey"), 0)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"doc": doc})
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	tasks, err := h.Store.ListTasks(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
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
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Status != "" {
		if err := ValidateTaskStatus(body.Status); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
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
	writeJSON(w, http.StatusCreated, map[string]any{"task": task})
}

func (h *Handler) patchTask(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
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
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.Title != nil {
		if err := ValidateTitle(*body.Title); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	in := TaskUpdateInput{Status: body.Status, Title: body.Title, Detail: body.Detail}
	if body.SessionID != nil && *body.SessionID != "" {
		if _, ok := h.resolveSession(w, r, user.Username, *body.SessionID); !ok {
			return
		}
		in.SessionID = body.SessionID
	}
	task, err := h.Store.UpdateTask(r.Context(), user.Username, r.PathValue("key"), in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

// resolveSession checks that a client-supplied sessionId belongs to the caller
// and returns the assistant bound to it. A missing or foreign session is a
// client mistake (400) rather than 403/404: the response must not confirm that
// someone else's session id exists.
func (h *Handler) resolveSession(w http.ResponseWriter, r *http.Request, userID, sessionID string) (string, bool) {
	assistantID, err := h.Store.SessionAssistant(r.Context(), userID, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "sessionId does not belong to the caller")
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
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeErr(w, http.StatusInternalServerError, err.Error())
}

// decodeBody reads a size-capped JSON body into v. An empty body decodes to the
// zero struct so PATCH may send only the fields it changes.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
