package assistant

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/policy"
)

type activityItem struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"` // tool | event | permission | denial | message
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	Status    string    `json:"status,omitempty"`
	Source    string    `json:"source"` // session | denial
}

func (h *Handler) activity(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		return
	}
	a, err := h.ownedAssistant(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeAssistantErr(w, err)
		return
	}
	var items []activityItem
	if h.Sessions != nil {
		list, err := h.Sessions.ListByAssistant(r.Context(), user.Username, a.ID, 1)
		if err == nil && len(list) > 0 {
			msgs, err := h.Sessions.ListMessages(r.Context(), list[0].ID, 80)
			if err == nil {
				for _, m := range msgs {
					item := activityItem{
						ID: m.ID, At: m.CreatedAt, Title: m.Content, Source: "session",
					}
					switch m.Role {
					case agentsession.RoleTool:
						item.Kind = "tool"
						var meta agentsession.ToolMeta
						if len(m.Meta) > 0 {
							_ = json.Unmarshal(m.Meta, &meta)
						}
						if meta.Title != "" {
							item.Title = meta.Title
						}
						item.Status = meta.Status
						item.Detail = truncate(m.Content, 200)
					case agentsession.RolePermission:
						item.Kind = "permission"
					case agentsession.RoleEvent:
						item.Kind = "event"
					case agentsession.RoleUser:
						item.Kind = "message"
						item.Title = "用户：" + truncate(m.Content, 80)
					case agentsession.RoleAssistant:
						item.Kind = "message"
						item.Title = "助手：" + truncate(m.Content, 80)
					default:
						continue
					}
					items = append(items, item)
				}
			}
		}
	}
	if h.Denials != nil {
		denials, err := h.Denials.ListByAssistant(r.Context(), a.ID, 30)
		if err == nil {
			for _, d := range denials {
				items = append(items, activityItem{
					ID: d.ID, At: d.CreatedAt, Kind: "denial", Source: "denial",
					Title:  d.Dimension + " · " + d.Target,
					Detail: d.Reason,
					Status: "denied",
				})
			}
		}
	}
	// newest first-ish: denials already desc; session msgs are oldest-first typically — reverse msgs already appended in order
	writeJSON(w, http.StatusOK, map[string]any{
		"activity": items,
		"busy":     activityBusy(items),
	})
}

func activityBusy(items []activityItem) bool {
	for _, it := range items {
		if it.Kind == "tool" && (it.Status == "pending" || it.Status == "in_progress" || it.Status == "running") {
			return true
		}
	}
	return false
}

func (h *Handler) policyCheck(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		return
	}
	a, err := h.ownedAssistant(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeAssistantErr(w, err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var body struct {
		Dimension string `json:"dimension"` // network | directory | capability
		Target    string `json:"target"`
		Mode      string `json:"mode"` // for directory: read|readwrite
		SessionID string `json:"sessionId"`
		Record    bool   `json:"record"` // persist denial when blocked
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var d policy.Decision
	prof := policyProfile(a)
	switch body.Dimension {
	case "network":
		d = policy.CheckNetwork(prof, body.Target)
	case "directory":
		d = policy.CheckDirectory(prof, body.Target, body.Mode)
	case "capability":
		d = policy.CheckCapability(prof, body.Target)
	default:
		writeErr(w, http.StatusBadRequest, "dimension must be network, directory, or capability")
		return
	}
	if !d.Allowed && body.Record && h.Denials != nil {
		_, _ = h.Denials.Record(r.Context(), user.Username, a.ID, body.SessionID, d)
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) listTickets(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		return
	}
	a, err := h.ownedAssistant(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeAssistantErr(w, err)
		return
	}
	if h.Tickets == nil {
		writeJSON(w, http.StatusOK, map[string]any{"tickets": []*assistticket.Ticket{}})
		return
	}
	pendingOnly := r.URL.Query().Get("pending") == "1"
	list, err := h.Tickets.ListByAssistant(r.Context(), user.Username, a.ID, pendingOnly)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*assistticket.Ticket{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": list})
}

func (h *Handler) createTicket(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		return
	}
	a, err := h.ownedAssistant(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeAssistantErr(w, err)
		return
	}
	if h.Tickets == nil {
		writeErr(w, http.StatusServiceUnavailable, "tickets unavailable")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var body struct {
		SessionID      string `json:"sessionId"`
		Kind           string `json:"kind"`
		Title          string `json:"title"`
		Reason         string `json:"reason"`
		ContextSummary string `json:"contextSummary"`
		AskHuman       string `json:"askHuman"`
		Payload        any    `json:"payload"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	t, err := h.Tickets.Create(r.Context(), user.Username, assistticket.CreateInput{
		AssistantID:    a.ID,
		SessionID:      body.SessionID,
		Kind:           body.Kind,
		Title:          body.Title,
		Reason:         body.Reason,
		ContextSummary: body.ContextSummary,
		AskHuman:       body.AskHuman,
		Payload:        body.Payload,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (h *Handler) resolveTicket(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		return
	}
	if h.Tickets == nil {
		writeErr(w, http.StatusServiceUnavailable, "tickets unavailable")
		return
	}
	t, err := h.Tickets.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, assistticket.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t.UserID != user.Username && user.Role != "admin" {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var body struct {
		Resolution string `json:"resolution"` // allow_once | permanent | reject
		Note       string `json:"note"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	switch body.Resolution {
	case assistticket.ResAllowOnce, assistticket.ResPermanent, assistticket.ResReject:
	default:
		writeErr(w, http.StatusBadRequest, "resolution must be allow_once, permanent, or reject")
		return
	}
	if body.Resolution == assistticket.ResPermanent {
		if err := h.applyPermanent(r, t); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	out, err := h.Tickets.Resolve(r.Context(), t.ID, body.Resolution, body.Note)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) applyPermanent(r *http.Request, t *assistticket.Ticket) error {
	var payload struct {
		Dimension string `json:"dimension"`
		Target    string `json:"target"`
		Mode      string `json:"mode"`
		Cap       string `json:"capability"`
	}
	if len(t.Payload) > 0 {
		_ = json.Unmarshal(t.Payload, &payload)
	}
	a, err := h.Store.Get(r.Context(), t.AssistantID)
	if err != nil {
		return err
	}
	in := UpdateInput{}
	switch payload.Dimension {
	case "network":
		if payload.Target != "" {
			list := append([]string{}, a.NetworkAllowlist...)
			list = append(list, payload.Target)
			in.NetworkAllowlist = &list
		}
	case "directory":
		mode := payload.Mode
		if mode == "" {
			mode = "read"
		}
		grants := append([]DirectoryGrant{}, a.DirectoryGrants...)
		grants = append(grants, DirectoryGrant{Path: payload.Target, Mode: mode, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
		in.DirectoryGrants = &grants
	case "capability":
		caps := a.Capabilities
		switch payload.Cap {
		case "shell":
			caps.Shell = true
		case "browser":
			caps.Browser = true
		}
		in.Capabilities = &caps
	default:
		// permission tickets may not mutate constitution
		return nil
	}
	_, err = h.Store.Update(r.Context(), a.ID, in)
	return err
}

func (h *Handler) pendingTickets(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Tickets == nil {
		writeJSON(w, http.StatusOK, map[string]any{"count": 0, "tickets": []*assistticket.Ticket{}})
		return
	}
	list, err := h.Tickets.ListPendingByUser(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*assistticket.Ticket{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "tickets": list})
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func policyProfile(a *Assistant) *policy.Profile {
	if a == nil {
		return nil
	}
	grants := make([]policy.DirGrant, 0, len(a.DirectoryGrants))
	for _, g := range a.DirectoryGrants {
		grants = append(grants, policy.DirGrant{Path: g.Path, Mode: g.Mode})
	}
	return &policy.Profile{
		Capabilities: policy.Caps{
			Shell: a.Capabilities.Shell, Browser: a.Capabilities.Browser,
			Mobile: a.Capabilities.Mobile, Desktop: a.Capabilities.Desktop,
		},
		NetworkTier:      a.NetworkTier,
		NetworkAllowlist: a.NetworkAllowlist,
		DirectoryGrants:  grants,
	}
}
