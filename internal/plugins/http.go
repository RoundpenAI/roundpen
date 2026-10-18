package plugins

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
)

// Handler serves the admin plugin catalog.
type Handler struct {
	Svc *Service
}

// Mount registers admin plugin routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/admin/plugins", auth.RequireAdmin(h.list))
	mux.HandleFunc("PUT /v1/admin/plugins/{id}", auth.RequireAdmin(h.set))
}

func (h *Handler) list(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"plugins": h.Svc.List()})
}

func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	id := r.PathValue("id")
	if _, ok := Lookup(id); !ok {
		httpx.WriteErr(w, http.StatusNotFound, "plugin not found")
		return
	}
	if err := h.Svc.SetEnabled(r.Context(), id, body.Enabled); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, p := range h.Svc.List() {
		if p.ID == id {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"plugin": p})
			return
		}
	}
	httpx.WriteErr(w, http.StatusNotFound, "plugin not found")
}
