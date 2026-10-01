package routine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/issue"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Handler serves /v1/routines.
type Handler struct {
	Store      *Store
	Assistants *assistant.Store
	Sessions   *agentsession.Store
	Issues     *issue.Store
	Tickets    *assistticket.Store
	// OnFinished runs after FinishRun commits, so delivery stays next to the write.
	OnFinished func(ctx context.Context, run *Run)
}

// Mount registers routine routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/routines", h.list)
	mux.HandleFunc("POST /v1/routines", h.create)
	mux.HandleFunc("GET /v1/routines/{key}", h.get)
	mux.HandleFunc("PATCH /v1/routines/{key}", h.patch)
	mux.HandleFunc("PUT /v1/routines/{key}/state", h.putState)
	mux.HandleFunc("GET /v1/routines/{key}/runs", h.listRuns)
	mux.HandleFunc("GET /v1/routines/{key}/runs/{runKey}", h.getRun)
	mux.HandleFunc("POST /v1/routines/{key}/runs/{runKey}/cancel", h.cancel)
	mux.HandleFunc("POST /v1/routines/runs/{runKey}/finish", h.finish)
	mux.HandleFunc("POST /v1/routines/runs/{runKey}/confirm", h.confirm)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "routine store unavailable")
		return
	}
	f := ListFilter{AssistantID: r.URL.Query().Get("assistantId"), Status: r.URL.Query().Get("status")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		f.Limit = n
	}
	if f.Status != "" {
		if err := validateStatus(f.Status); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	items, err := h.Store.List(r.Context(), user.Username, f)
	if err != nil {
		writeErr(w, err)
		return
	}
	if items == nil {
		items = []Routine{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"routines": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Title               string   `json:"title"`
		Brief               string   `json:"brief"`
		Cron                string   `json:"cron"`
		Timezone            string   `json:"timezone"`
		Autonomy            string   `json:"autonomy"`
		Hosts               []string `json:"hosts"`
		DeliverIM           *bool    `json:"deliverIm"`
		MaxDurationSec      int      `json:"maxDurationSec"`
		AssigneeAssistantID string   `json:"assigneeAssistantId"`
		IssueKey            string   `json:"issueKey"`
		SessionID           string   `json:"sessionId"`
		AssigneeConfirmed   bool     `json:"assigneeConfirmed"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	var createdByAssistant, createdBySession string
	if body.SessionID != "" {
		asst, ok := h.sessionAssistant(w, r, user.Username, body.SessionID)
		if !ok {
			return
		}
		createdByAssistant = asst
		createdBySession = body.SessionID
		if body.AssigneeAssistantID == "" {
			body.AssigneeAssistantID = asst
		}
	}
	if body.AssigneeAssistantID == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "assigneeAssistantId is required")
		return
	}
	if body.AssigneeAssistantID != createdByAssistant && !body.AssigneeConfirmed {
		httpx.WriteErr(w, http.StatusBadRequest, "assignee must be confirmed")
		return
	}
	assignee, ok := h.loadAssignee(w, r, user.Username, body.AssigneeAssistantID)
	if !ok {
		return
	}
	if err := assigneeCan(assignee, body.Autonomy); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var issueID string
	if body.IssueKey != "" {
		id, ok := h.resolveIssue(w, r, user.Username, body.IssueKey)
		if !ok {
			return
		}
		issueID = id
	}
	deliver := false
	if body.DeliverIM != nil {
		deliver = *body.DeliverIM
	} else {
		deliver = assignee.ImChannels.HasEnabledChannel()
	}
	rt, err := h.Store.Create(r.Context(), user.Username, CreateInput{
		AssigneeAssistantID:  assignee.ID,
		CreatedByAssistantID: createdByAssistant,
		CreatedBySessionID:   createdBySession,
		IssueID:              issueID,
		Title:                body.Title,
		Brief:                body.Brief,
		Autonomy:             body.Autonomy,
		Hosts:                body.Hosts,
		Cron:                 body.Cron,
		Timezone:             body.Timezone,
		DeliverIM:            deliver,
		MaxDurationSec:       body.MaxDurationSec,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"routine": rt})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	user := mustUser(w, r)
	if user == nil {
		return
	}
	rt, runs, err := h.Store.Get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if runs == nil {
		runs = []Run{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"routine": rt, "runs": runs})
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	user := mustUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		Title               *string  `json:"title"`
		Brief               *string  `json:"brief"`
		Autonomy            *string  `json:"autonomy"`
		Hosts               []string `json:"hosts"`
		Cron                *string  `json:"cron"`
		Timezone            *string  `json:"timezone"`
		DeliverIM           *bool    `json:"deliverIm"`
		MaxDurationSec      *int     `json:"maxDurationSec"`
		Status              *string  `json:"status"`
		AssigneeAssistantID *string  `json:"assigneeAssistantId"`
		IssueKey            *string  `json:"issueKey"`
		AssigneeConfirmed   bool     `json:"assigneeConfirmed"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	cur, err := h.Store.get(r.Context(), user.Username, r.PathValue("key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	in := UpdateInput{
		Title: body.Title, Brief: body.Brief, Autonomy: body.Autonomy,
		Cron: body.Cron, Timezone: body.Timezone, DeliverIM: body.DeliverIM,
		MaxDurationSec: body.MaxDurationSec, Status: body.Status,
	}
	if body.Hosts != nil {
		in.Hosts = body.Hosts
		in.HostsSet = true
	}
	autonomy := cur.Autonomy
	if body.Autonomy != nil {
		autonomy = *body.Autonomy
	}
	assigneeID := cur.AssigneeAssistantID
	if body.AssigneeAssistantID != nil {
		if *body.AssigneeAssistantID != cur.AssigneeAssistantID && !body.AssigneeConfirmed {
			httpx.WriteErr(w, http.StatusBadRequest, "assignee must be confirmed")
			return
		}
		assignee, ok := h.loadAssignee(w, r, user.Username, *body.AssigneeAssistantID)
		if !ok {
			return
		}
		assigneeID = assignee.ID
		in.AssigneeAssistantID = &assignee.ID
	}
	if body.Autonomy != nil || body.AssigneeAssistantID != nil {
		assignee, ok := h.loadAssignee(w, r, user.Username, assigneeID)
		if !ok {
			return
		}
		if err := assigneeCan(assignee, autonomy); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.IssueKey != nil {
		in.IssueSet = true
		if strings.TrimSpace(*body.IssueKey) == "" {
			in.IssueID = nil
		} else {
			id, ok := h.resolveIssue(w, r, user.Username, *body.IssueKey)
			if !ok {
				return
			}
			in.IssueID = &id
		}
	}
	rt, err := h.Store.Update(r.Context(), user.Username, cur.Key, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"routine": rt})
}

func (h *Handler) putState(w http.ResponseWriter, r *http.Request) {
	user := mustUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		State     json.RawMessage `json:"state"`
		SessionID string          `json:"sessionId"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	only := ""
	if body.SessionID != "" {
		asst, ok := h.sessionAssistant(w, r, user.Username, body.SessionID)
		if !ok {
			return
		}
		only = asst
	}
	rt, err := h.Store.UpdateState(r.Context(), user.Username, r.PathValue("key"), body.State, only)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"routine": rt})
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	user := mustUser(w, r)
	if user == nil {
		return
	}
	limit := 30
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = n
	}
	runs, err := h.Store.ListRuns(r.Context(), user.Username, r.PathValue("key"), limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	if runs == nil {
		runs = []Run{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	user := mustUser(w, r)
	if user == nil {
		return
	}
	rn, err := h.Store.GetRun(r.Context(), user.Username, r.PathValue("key"), r.PathValue("runKey"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"run": rn})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	user := mustUser(w, r)
	if user == nil {
		return
	}
	rn, err := h.Store.CancelRun(r.Context(), user.Username, r.PathValue("key"), r.PathValue("runKey"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"run": rn})
}

func (h *Handler) finish(w http.ResponseWriter, r *http.Request) {
	user := mustUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		SessionID string     `json:"sessionId"`
		Summary   string     `json:"summary"`
		Artifacts []Artifact `json:"artifacts"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	rn, err := h.Store.FinishRun(r.Context(), user.Username, r.PathValue("runKey"), body.SessionID, body.Summary, body.Artifacts)
	if err != nil {
		writeErr(w, err)
		return
	}
	if h.OnFinished != nil {
		h.OnFinished(r.Context(), rn)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"run": rn})
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	user := mustUser(w, r)
	if user == nil {
		return
	}
	if h.Tickets == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "assist tickets unavailable")
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
		Title     string `json:"title"`
		Reason    string `json:"reason"`
		AskHuman  string `json:"askHuman"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	run, err := h.Store.GetRunByKey(r.Context(), user.Username, r.PathValue("runKey"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if run.SessionID != body.SessionID || run.Status != RunRunning {
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
		return
	}
	rt, err := h.Store.get(r.Context(), user.Username, run.RoutineID)
	if err != nil {
		writeErr(w, err)
		return
	}
	left := rt.MaxDurationSec
	if run.StartedAt != nil {
		used := int(time.Since(*run.StartedAt).Seconds())
		left = rt.MaxDurationSec - used
	}
	ticket, err := h.Tickets.Create(r.Context(), user.Username, assistticket.CreateInput{
		AssistantID: run.AssistantID,
		SessionID:   run.SessionID,
		Kind:        assistticket.KindOther,
		Title:       body.Title,
		Reason:      body.Reason,
		AskHuman:    body.AskHuman,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	updated, err := h.Store.RequestConfirm(r.Context(), user.Username, run.Key, body.SessionID, ticket.ID, left)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"run": updated, "paused": true})
}

func assigneeCan(a *assistant.Assistant, autonomy string) error {
	if a.Status != assistant.StatusActive {
		return errors.New("assignee is not active")
	}
	if a.NetworkTier == assistant.NetworkNone {
		return errors.New("assignee network is disabled")
	}
	if autonomy == AutonomyBrowse && !a.Capabilities.Browser {
		return errors.New("assignee does not have browser")
	}
	return nil
}

func (h *Handler) loadAssignee(w http.ResponseWriter, r *http.Request, userID, id string) (*assistant.Assistant, bool) {
	if h.Assistants == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "assistants unavailable")
		return nil, false
	}
	a, err := h.Assistants.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, assistant.ErrNotFound) {
			httpx.WriteErr(w, http.StatusBadRequest, "assignee not found")
			return nil, false
		}
		writeErr(w, err)
		return nil, false
	}
	if a.UserID != userID {
		httpx.WriteErr(w, http.StatusBadRequest, "assignee not found")
		return nil, false
	}
	return a, true
}

func (h *Handler) sessionAssistant(w http.ResponseWriter, r *http.Request, userID, sessionID string) (string, bool) {
	if h.Sessions == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "sessions unavailable")
		return "", false
	}
	sess, err := h.Sessions.Get(r.Context(), sessionID)
	if err != nil || sess.UserID != userID {
		httpx.WriteErr(w, http.StatusBadRequest, "sessionId does not belong to the caller")
		return "", false
	}
	if sess.AssistantID == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "session has no assistant")
		return "", false
	}
	return sess.AssistantID, true
}

func (h *Handler) resolveIssue(w http.ResponseWriter, r *http.Request, userID, key string) (string, bool) {
	if h.Issues == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "issues unavailable")
		return "", false
	}
	it, _, _, err := h.Issues.Get(r.Context(), userID, key)
	if err != nil {
		if errors.Is(err, issue.ErrNotFound) {
			httpx.WriteErr(w, http.StatusBadRequest, "issue not found")
			return "", false
		}
		writeErr(w, err)
		return "", false
	}
	return it.ID, true
}

func mustUser(w http.ResponseWriter, r *http.Request) *storage.User {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return nil
	}
	return user
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrForbidden):
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, ErrInvalid):
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
	default:
		httpx.WriteErrOrInternal(w, nil, err, nil)
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}
