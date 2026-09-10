package gitcred

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
)

// Handler serves /v1/me/git-credentials.
type Handler struct {
	Store *Store
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/git-credentials", h.list)
	mux.HandleFunc("PUT /v1/me/git-credentials", h.upsert)
	mux.HandleFunc("DELETE /v1/me/git-credentials/{id}", h.delete)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		writeJSON(w, http.StatusOK, map[string]any{"credentials": []Cred{}})
		return
	}
	list, err := h.Store.List(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]Cred, 0, len(list))
	for _, c := range list {
		out = append(out, c.SanitizeForResponse())
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "git credentials not configured")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var in UpsertInput
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.Store.Upsert(r.Context(), user.Username, in)
	if err != nil {
		if strings.Contains(err.Error(), "required") {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c.SanitizeForResponse())
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.Store.Delete(r.Context(), user.Username, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
