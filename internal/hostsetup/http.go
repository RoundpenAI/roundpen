package hostsetup

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

type Handler struct {
	Svc *Service
	Cfg *config.Config
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/setup/llm-ready", h.llmReady)
	mux.HandleFunc("POST /v1/setup/plans", h.createPlan)
	mux.HandleFunc("GET /v1/setup/plans/{id}", h.getPlan)
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/confirm", h.confirm)
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/recheck", h.recheck)
	mux.HandleFunc("POST /v1/setup/plans/{id}/actions/{actionId}/retry", h.retry)
}

func (h *Handler) llmReady(w http.ResponseWriter, r *http.Request) {
	if auth.GetUser(r.Context()) == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ready, reason := LLMReady(h.Cfg)
	writeJSON(w, http.StatusOK, map[string]any{"ready": ready, "reason": reason})
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "setup not configured")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var ctx WizardContext
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &ctx); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	rec, err := h.Svc.CreatePlan(r.Context(), user.Username, ctx)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
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
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "setup not configured")
		return
	}
	id := r.PathValue("id")
	rec, err := fn(user.Username, id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Handler) withAction(w http.ResponseWriter, r *http.Request, fn func(user, id, actionID string) (*PlanRecord, error)) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "setup not configured")
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
		writeErr(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func TestLLMReadyUnauthorized(t *testing.T) {
	mux := http.NewServeMux()
	(&Handler{Cfg: &config.Config{}}).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/setup/llm-ready", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", rr.Code)
	}
}

func TestCreatePlanEmptyActions(t *testing.T) {
	svc := &Service{
		Facts: func() HostFacts {
			return HostFacts{BinariesOK: true, AgentImageOK: true, BrowserImageOK: true}
		},
	}
	mux := http.NewServeMux()
	(&Handler{Svc: svc, Cfg: &config.Config{}}).Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/setup/plans", strings.NewReader(`{"preset":"code"}`))
	req = req.WithContext(auth.WithUser(req.Context(), &storage.User{Username: "alice"}))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var rec PlanRecord
	if err := json.Unmarshal(rr.Body.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.Actions) != 0 {
		t.Fatalf("%#v", rec.Actions)
	}
}
