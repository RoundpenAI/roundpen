package settings

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
)

// Handler serves admin settings endpoints.
type Handler struct {
	Svc *Service
}

type settingsResp struct {
	Settings AppSettings `json:"settings"`
	System   SystemInfo  `json:"system"`
}

// Mount registers admin settings routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/admin/settings", auth.RequireAdmin(h.get))
	mux.HandleFunc("PUT /v1/admin/settings", auth.RequireAdmin(h.put))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	settings, system := h.Svc.Response()
	writeJSON(w, http.StatusOK, settingsResp{
		Settings: settings,
		System:   system,
	})
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	next, err := DecodeAppSettings(raw, h.Svc.Current())
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.Svc.Update(r.Context(), next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	settings, system := h.Svc.Response()
	writeJSON(w, http.StatusOK, settingsResp{
		Settings: settings,
		System:   system,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}
