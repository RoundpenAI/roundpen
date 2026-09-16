// Package envapi exposes fixed per-user environments (agent / browser / mobile).
package envapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// SandboxDialer opens a TCP connection to a port inside a sandbox.
type SandboxDialer interface {
	Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)
}

// Environments is the userenv surface used by environment HTTP handlers.
type Environments interface {
	List(ctx context.Context, userID string) ([]userenv.EnvView, error)
	EnsureBrowser(ctx context.Context, userID string) (*userenv.BrowserTarget, error)
	EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
	UpgradeAgent(ctx context.Context, userID string, force bool) (*userenv.UpgradeResult, error)
	RecreateAgent(ctx context.Context, userID string) (*userenv.UpgradeResult, error)
	RecreateBrowser(ctx context.Context, userID string) (*userenv.UpgradeResult, error)
}

// UserPrefs persists per-user environment preferences.
type UserPrefs interface {
	SetModelSource(ctx context.Context, username, source string) error
	SetSlotProxy(ctx context.Context, username, slot, profileID string) error
}

// Handler serves /v1/me/environments*, /v1/me/model-source and /v1/me/proxies.
type Handler struct {
	Envs  Environments
	Users UserPrefs
	Cfg   *config.Config
	Dial  SandboxDialer
	// Proxies lists admin-defined proxy profiles (nil = none configured).
	Proxies func() []settings.ProxyProfile

	liveMu    sync.Mutex
	liveCache map[string]liveTarget // username -> resolved browser container
}

// Mount registers environment routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/environments", h.list)
	mux.HandleFunc("POST /v1/me/environments/browser/ensure", h.ensureBrowser)
	mux.HandleFunc("POST /v1/me/environments/agent/ensure", h.ensureAgent)
	mux.HandleFunc("POST /v1/me/environments/agent/upgrade", h.upgradeAgent)
	mux.HandleFunc("GET /v1/me/environments/browser/live-link", h.liveLink)
	mux.HandleFunc("GET /v1/me/model-source", h.getModelSource)
	mux.HandleFunc("PUT /v1/me/model-source", h.putModelSource)
	mux.HandleFunc("GET /v1/me/proxies", h.getProxies)
	mux.HandleFunc("PUT /v1/me/proxy", h.putProxy)
	// The subtree pattern also redirects /live to /live/ (query preserved).
	mux.HandleFunc(liveRoutePrefix+"/", h.live)
}

// proxyView is a selection-facing profile (no URLs: they may carry creds).
type proxyView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (h *Handler) getProxies(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	views := []proxyView{}
	if h.Proxies != nil {
		for _, p := range h.Proxies() {
			views = append(views, proxyView{ID: p.ID, Name: p.Name, Description: p.Description})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"proxies": views,
		"agent":   user.AgentProxy,
		"browser": user.BrowserProxy,
	})
}

func (h *Handler) putProxy(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Slot      string `json:"slot"`
		ProfileID string `json:"profileId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	slot := strings.TrimSpace(body.Slot)
	if slot != userenv.SlotAgent && slot != userenv.SlotBrowser {
		writeErr(w, http.StatusBadRequest, "slot must be agent or browser")
		return
	}
	profileID := strings.TrimSpace(body.ProfileID)
	if profileID != "" {
		found := false
		if h.Proxies != nil {
			for _, p := range h.Proxies() {
				if p.ID == profileID {
					found = true
					break
				}
			}
		}
		if !found {
			writeErr(w, http.StatusBadRequest, "unknown proxy profile")
			return
		}
	}
	if h.Users == nil {
		writeErr(w, http.StatusServiceUnavailable, "user store not configured")
		return
	}
	if err := h.Users.SetSlotProxy(r.Context(), user.Username, slot, profileID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := map[string]any{"slot": slot, "profileId": profileID}
	// Rebuild so the new env applies: sandbox env is fixed at creation.
	current := user.AgentProxy
	if slot == userenv.SlotBrowser {
		current = user.BrowserProxy
	}
	if current != profileID && h.Envs != nil {
		var res *userenv.UpgradeResult
		var err error
		if slot == userenv.SlotAgent {
			res, err = h.Envs.RecreateAgent(r.Context(), user.Username)
		} else {
			res, err = h.Envs.RecreateBrowser(r.Context(), user.Username)
		}
		if err != nil {
			// The preference is saved; surface the rebuild failure without
			// failing the request (the upgrade button can retry).
			log.Printf("proxy rebuild for %s/%s: %v", user.Username, slot, err)
			resp["rebuildError"] = err.Error()
		} else {
			resp["status"] = res.Status
			resp["environment"] = res.Environment
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getModelSource(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	src := user.ModelSource
	if src == "" {
		src = storage.ModelSourceGateway
	}
	writeJSON(w, http.StatusOK, map[string]any{"modelSource": src})
}

func (h *Handler) putModelSource(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		ModelSource string `json:"modelSource"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	src := strings.TrimSpace(body.ModelSource)
	if !storage.ValidModelSource(src) {
		writeErr(w, http.StatusBadRequest, "modelSource must be gateway or own")
		return
	}
	if h.Users == nil {
		writeErr(w, http.StatusServiceUnavailable, "user store not configured")
		return
	}
	if err := h.Users.SetModelSource(r.Context(), user.Username, src); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := map[string]any{"modelSource": src}
	// Rebuild so the new env applies: sandbox env is fixed at creation.
	if user.ModelSource != src && h.Envs != nil {
		res, err := h.Envs.RecreateAgent(r.Context(), user.Username)
		if err != nil {
			// The preference is saved; surface the rebuild failure without
			// failing the request (the upgrade button can retry).
			log.Printf("model-source rebuild for %s: %v", user.Username, err)
			resp["rebuildError"] = err.Error()
		} else {
			resp["status"] = res.Status
			resp["environment"] = res.Environment
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	list, err := h.Envs.List(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": list})
}

func (h *Handler) ensureBrowser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	target, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := map[string]any{
		"slot":     userenv.SlotBrowser,
		"provider": target.Provider,
		"managed":  target.Managed,
	}
	if target.Sandbox != nil {
		resp["sandboxId"] = target.Sandbox.ID
		resp["status"] = string(target.Sandbox.Status)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ensureAgent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	sb, err := h.Envs.EnsureAgent(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slot":      userenv.SlotAgent,
		"sandboxId": sb.ID,
		"status":    sb.Status,
		"name":      sb.Name,
	})
}

func (h *Handler) upgradeAgent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	var body struct {
		Force bool `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := h.Envs.UpgradeAgent(r.Context(), user.Username, body.Force)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      res.Status,
		"image":       res.Image,
		"digest":      res.Digest,
		"environment": res.Environment,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
