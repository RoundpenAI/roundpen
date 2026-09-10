package runtime

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
)

// Handler serves /v1/runtime for any signed-in user.
type Handler struct {
	Probe *Probe
	Prefs *PrefStore
}

type putReq struct {
	AgentEngine string `json:"agentEngine"`
}

// Mount registers runtime routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/runtime", h.get)
	mux.HandleFunc("PUT /v1/runtime", h.put)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	snap := Snapshot{DefaultAgentEngine: EngineQEMU}
	if h.Probe != nil {
		snap = h.Probe.Snapshot()
	}
	engine := snap.DefaultAgentEngine
	if h.Prefs != nil {
		if got, err := h.Prefs.ResolveAgentEngine(r.Context(), user.Username, snap.DefaultAgentEngine); err == nil {
			engine = got
		}
	}
	snap.AgentEngine = engine
	writeJSON(w, http.StatusOK, snap)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req putReq
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if h.Prefs == nil {
		writeErr(w, http.StatusServiceUnavailable, "runtime preferences not configured")
		return
	}
	if err := h.Prefs.SetAgentEngine(r.Context(), user.Username, req.AgentEngine); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.get(w, r)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// WriteNotReady writes a 409 payload the UI can render as a setup guide.
func WriteNotReady(w http.ResponseWriter, err error) bool {
	n := AsNotReady(err)
	if n == nil {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":  n.Error(),
		"code":   "engine_not_ready",
		"engine": n.Engine,
		"setup":  n.Setup,
	})
	return true
}
