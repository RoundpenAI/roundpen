package download

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
)

// Handler serves /v1/me/download-backends.
type Handler struct {
	Store *Store
}

// Mount registers routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	if h == nil || h.Store == nil || mux == nil {
		return
	}
	mux.HandleFunc("GET /v1/me/download-backends", h.list)
	mux.HandleFunc("POST /v1/me/download-backends", h.upsert)
	mux.HandleFunc("PUT /v1/me/download-backends/{id}", h.upsert)
	mux.HandleFunc("DELETE /v1/me/download-backends/{id}", h.delete)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	list, err := h.Store.List(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	if list == nil {
		list = []Backend{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"backends": list})
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
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
		Name            string  `json:"name"`
		Type            string  `json:"type"`
		BaseURL         string  `json:"baseUrl"`
		Username        string  `json:"username"`
		Password        *string `json:"password"`
		DefaultSavePath string  `json:"defaultSavePath"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	b, err := h.Store.Upsert(r.Context(), user.Username, UpsertInput{
		ID: id, Name: in.Name, Type: in.Type, BaseURL: in.BaseURL,
		Username: in.Username, Password: in.Password, DefaultSavePath: in.DefaultSavePath,
	})
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, map[string]any{"backend": b})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := h.Store.Delete(r.Context(), user.Username, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteErr(w, http.StatusNotFound, "not found")
			return
		}
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
