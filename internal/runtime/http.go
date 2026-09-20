package runtime

import (
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
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
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	snap := Snapshot{AgentImage: "roundpen-code-agent:local"}
	if h.Probe != nil {
		snap = h.Probe.Snapshot()
	}
	httpx.WriteJSON(w, http.StatusOK, snap)
}

// WriteNotReady writes a 409 payload the UI can render as a setup guide.
func WriteNotReady(w http.ResponseWriter, err error) bool {
	n := AsNotReady(err)
	if n == nil {
		return false
	}
	httpx.WriteJSON(w, http.StatusConflict, map[string]any{
		"error":  n.Error(),
		"code":   "engine_not_ready",
		"engine": n.Engine,
		"setup":  n.Setup,
	})
	return true
}
