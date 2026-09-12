package runtime

import (
	"encoding/json"
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
)

// Handler serves /v1/runtime for any signed-in user.
type Handler struct {
	Probe *Probe
}

// Mount registers runtime routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/runtime", h.get)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	snap := Snapshot{AgentImage: "roundpen-code-agent:local"}
	if h.Probe != nil {
		snap = h.Probe.Snapshot()
	}
	writeJSON(w, http.StatusOK, snap)
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
