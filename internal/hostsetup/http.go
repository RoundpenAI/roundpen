package hostsetup

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/httpx"
)

type Handler struct {
	Svc *Service
	Cfg *config.Config
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/setup/llm-ready", h.llmReady)
	mux.HandleFunc("POST /v1/setup/plans", h.createPlan)
	mux.HandleFunc("GET /v1/setup/plans/{id}", h.getPlan)
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/confirm", auth.RequireAdmin(h.confirm))
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/recheck", h.recheck)
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/retry", auth.RequireAdmin(h.retry))
}

func (h *Handler) llmReady(w http.ResponseWriter, r *http.Request) {
	if auth.GetUser(r.Context()) == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ready, reason := LLMReady(h.Cfg)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ready": ready, "reason": reason})
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "setup not configured")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var ctx WizardContext
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &ctx); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	rec, err := h.Svc.CreatePlan(r.Context(), user.Username, ctx)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}

func (h *Handler) getPlan(w http.ResponseWriter, r *http.Request) {
	h.withPlan(w, r, func(user, id string) (*PlanRecord, error) {
		return h.Svc.Get(user, id)
	})
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	h.withAction(w, r, h.Svc.Confirm)
}

func (h *Handler) recheck(w http.ResponseWriter, r *http.Request) {
	h.withAction(w, r, h.Svc.Recheck)
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	h.withAction(w, r, h.Svc.Retry)
}

func (h *Handler) withPlan(w http.ResponseWriter, r *http.Request, fn func(user, id string) (*PlanRecord, error)) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "setup not configured")
		return
	}
	id := r.PathValue("id")
	rec, err := fn(user.Username, id)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}

func (h *Handler) withAction(w http.ResponseWriter, r *http.Request, fn func(user, id, actionID string) (*PlanRecord, error)) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "setup not configured")
		return
	}
	id := r.PathValue("id")
	actionID := r.PathValue("actionId")
	rec, err := fn(user.Username, id, actionID)
	if err != nil {
		msg := err.Error()
		code := http.StatusBadRequest
		if strings.Contains(msg, "not found") {
			code = http.StatusNotFound
		}
		httpx.WriteErr(w, code, msg)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}
