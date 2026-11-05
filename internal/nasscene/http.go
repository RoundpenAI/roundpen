package nasscene

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
)

// Handler serves /v1/me/nas-scene.
type Handler struct {
	Store *Store
}

// Mount registers routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	if h == nil || h.Store == nil || mux == nil {
		return
	}
	mux.HandleFunc("GET /v1/me/nas-scene", h.get)
	mux.HandleFunc("PUT /v1/me/nas-scene", h.put)
	mux.HandleFunc("POST /v1/me/nas-scene/restore", h.restore)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	st, err := h.Store.Describe(r.Context(), user.Username, user.AuthProvider)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Enabled == nil {
		httpx.WriteErr(w, http.StatusBadRequest, "enabled boolean required")
		return
	}
	if err := h.Store.SetEnabled(r.Context(), user.Username, *in.Enabled); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	st, err := h.Store.Describe(r.Context(), user.Username, user.AuthProvider)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	enabled, err := h.Store.Enabled(r.Context(), user.Username, user.AuthProvider)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	if !enabled {
		httpx.WriteErr(w, http.StatusConflict, "enable the NAS scene pack first")
		return
	}
	if err := h.Store.RestoreMissing(r.Context(), user.Username); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	st, err := h.Store.Describe(r.Context(), user.Username, user.AuthProvider)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, st)
}
