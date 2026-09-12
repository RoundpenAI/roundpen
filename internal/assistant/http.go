package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/policy"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// SessionStarter starts an ACP session bound to an assistant.
type SessionStarter interface {
	StartForAssistant(ctx context.Context, user *storage.User, assistantID, title string) (*agentsession.Session, error)
}

// Handler serves /v1/assistants.
type Handler struct {
	Store    *Store
	Sessions *agentsession.Store
	Starter  SessionStarter
	Tickets  *assistticket.Store
	Denials  *policy.DenialStore
}

// Mount registers routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/assistants", h.list)
	mux.HandleFunc("POST /v1/assistants", h.create)
	mux.HandleFunc("GET /v1/assistants/{id}", h.get)
	mux.HandleFunc("PATCH /v1/assistants/{id}", h.patch)
	mux.HandleFunc("POST /v1/assistants/{id}/ensure-session", h.ensureSession)
	mux.HandleFunc("GET /v1/assistants/{id}/activity", h.activity)
	mux.HandleFunc("POST /v1/assistants/{id}/policy/check", h.policyCheck)
	mux.HandleFunc("GET /v1/assistants/{id}/assist-tickets", h.listTickets)
	mux.HandleFunc("POST /v1/assistants/{id}/assist-tickets", h.createTicket)
	mux.HandleFunc("POST /v1/assist-tickets/{id}/resolve", h.resolveTicket)
	mux.HandleFunc("GET /v1/me/assist-tickets/pending", h.pendingTickets)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	sys, err := h.Store.EnsureSystem(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.Sessions != nil && sys != nil {
		if _, err := h.Store.AttachOrphanSessions(r.Context(), user.Username, sys.ID); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	list, err := h.Store.ListByUser(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*Assistant{}
	}
	for _, a := range list {
		h.fillPrimarySession(r.Context(), user.Username, a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"assistants": list})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var body struct {
		Name         string        `json:"name"`
		Bio          string        `json:"bio"`
		IdentityMode string        `json:"identityMode"`
		Preset       string        `json:"preset"`
		Capabilities *Capabilities `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.IdentityMode == "" {
		body.IdentityMode = IdentityProxyUser
	}
	a, err := h.Store.Create(r.Context(), user.Username, CreateInput{
		Name:         body.Name,
		Bio:          body.Bio,
		IdentityMode: body.IdentityMode,
		Preset:       body.Preset,
		Capabilities: body.Capabilities,
	})
	if err != nil {
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	a, err := h.ownedAssistant(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeAssistantErr(w, err)
		return
	}
	h.fillPrimarySession(r.Context(), user.Username, a)
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	cur, err := h.ownedAssistant(r.Context(), user, id)
	if err != nil {
		writeAssistantErr(w, err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var body struct {
		Name                  *string           `json:"name"`
		Bio                   *string           `json:"bio"`
		IdentityMode          *string           `json:"identityMode"`
		ConfirmIdentityChange bool              `json:"confirmIdentityChange"`
		Capabilities          *Capabilities     `json:"capabilities"`
		NetworkTier           *string           `json:"networkTier"`
		NetworkAllowlist      *[]string         `json:"networkAllowlist"`
		DirectoryGrants       *[]DirectoryGrant `json:"directoryGrants"`
		Status                *string           `json:"status"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.IdentityMode != nil && *body.IdentityMode != cur.IdentityMode && !body.ConfirmIdentityChange {
		writeErr(w, http.StatusBadRequest, "confirmIdentityChange required to change identityMode")
		return
	}
	updated, err := h.Store.Update(r.Context(), id, UpdateInput{
		Name:             body.Name,
		Bio:              body.Bio,
		IdentityMode:     body.IdentityMode,
		Capabilities:     body.Capabilities,
		NetworkTier:      body.NetworkTier,
		NetworkAllowlist: body.NetworkAllowlist,
		DirectoryGrants:  body.DirectoryGrants,
		Status:           body.Status,
	})
	if err != nil {
		if errors.Is(err, ErrSystemUndeletable) {
			writeErr(w, http.StatusForbidden, "系统助手不可删除")
			return
		}
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.fillPrimarySession(r.Context(), user.Username, updated)
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) ensureSession(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	a, err := h.ownedAssistant(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeAssistantErr(w, err)
		return
	}
	if h.Sessions != nil {
		list, err := h.Sessions.ListByAssistant(r.Context(), user.Username, a.ID, 1)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(list) > 0 {
			existing := list[0]
			// Prefer an in-process provider when present. Sandbox-backed
			// sessions (e.g. legacy claude) are skipped so we can open a
			// sysadmin chat without QEMU for now.
			if meta, ok := providers.ByID(providers.Default(), existing.ProviderID); ok && !providers.NeedsSandbox(meta) {
				writeJSON(w, http.StatusOK, map[string]any{"sessionId": existing.ID})
				return
			}
		}
	}
	if h.Starter == nil {
		writeErr(w, http.StatusServiceUnavailable, "session starter not configured")
		return
	}
	sess, err := h.Starter.StartForAssistant(r.Context(), user, a.ID, a.Name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sess.ID})
}

func (h *Handler) fillPrimarySession(ctx context.Context, userID string, a *Assistant) {
	if h.Sessions == nil || a == nil {
		return
	}
	list, err := h.Sessions.ListByAssistant(ctx, userID, a.ID, 1)
	if err != nil || len(list) == 0 {
		return
	}
	a.PrimarySessionID = list[0].ID
}

func (h *Handler) ownedAssistant(ctx context.Context, user *storage.User, id string) (*Assistant, error) {
	a, err := h.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.UserID != user.Username && user.Role != storage.RoleAdmin {
		return nil, errForbidden
	}
	return a, nil
}

var errForbidden = errors.New("forbidden")

func writeAssistantErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	writeErr(w, http.StatusInternalServerError, err.Error())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
