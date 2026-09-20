package settings

import (
	"io"
	"net/http"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/httpx"
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
	mux.HandleFunc("POST /v1/admin/settings/browser/test", auth.RequireAdmin(h.browserTest))
	mux.HandleFunc("GET /v1/admin/settings/automode/defaults", auth.RequireAdmin(h.autoModeDefaults))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	settings, system := h.Svc.Response()
	httpx.WriteJSON(w, http.StatusOK, settingsResp{
		Settings: settings,
		System:   system,
	})
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	next, err := DecodeAppSettings(raw, h.Svc.Current())
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.Svc.Update(r.Context(), next); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	settings, system := h.Svc.Response()
	httpx.WriteJSON(w, http.StatusOK, settingsResp{
		Settings: settings,
		System:   system,
	})
}

// autoModeDefaults returns the built-in classifier rules so the settings UI
// can show what "$defaults" expands to.
func (h *Handler) autoModeDefaults(w http.ResponseWriter, r *http.Request) {
	def := automode.Defaults()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"environment": def.Environment,
		"allow":       def.Allow,
		"softDeny":    def.SoftDeny,
		"hardDeny":    def.HardDeny,
	})
}

func (h *Handler) browserTest(w http.ResponseWriter, r *http.Request) {
	res, err := h.Svc.TestBrowser(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "result": res})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}
