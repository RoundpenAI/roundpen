package hostsetup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

type Handler struct {
	Svc *Service
	Cfg *config.Config
	// Items is the setting-items catalog (LLM providers / slots).
	Items *settingitems.Catalog
	// EnableLLMGW turns on the gateway in persisted settings (admin FREE path).
	EnableLLMGW func(ctx context.Context) error
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/setup/llm-ready", h.llmReady)
	mux.HandleFunc("POST /v1/setup/llm-free", auth.RequireAdmin(h.enableFree))
	mux.HandleFunc("POST /v1/setup/plans", h.createPlan)
	mux.HandleFunc("GET /v1/setup/plans/{id}", h.getPlan)
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/confirm", auth.RequireAdmin(h.confirm))
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/recheck", h.recheck)
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/retry", auth.RequireAdmin(h.retry))
}

func (h *Handler) llmStatus() LLMStatus {
	var snap *settingitems.Snapshot
	if h.Items != nil {
		snap = h.Items.Snapshot()
	}
	return LLMReady(h.Cfg, snap)
}

func (h *Handler) llmReady(w http.ResponseWriter, r *http.Request) {
	if auth.GetUser(r.Context()) == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.llmStatus())
}

func (h *Handler) enableFree(w http.ResponseWriter, r *http.Request) {
	if h.Items == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "setting items not configured")
		return
	}
	env := llmgw.LoadFreeEnv()
	if h.EnableLLMGW != nil {
		if err := h.EnableLLMGW(r.Context()); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "enable llm gateway: "+err.Error())
			return
		}
	} else if h.Cfg != nil {
		h.Cfg.LLMGW.Enabled = true
	}
	if err := llmgw.SeedFree(r.Context(), h.Items, env); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.llmStatus())
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
