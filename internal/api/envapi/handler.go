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
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
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
	BrowserProfileFor(userID string) browser.Profile
}

// UserPrefs persists per-user environment preferences.
type UserPrefs interface {
	SetModelSource(ctx context.Context, username, source string) error
}

// Handler serves /v1/me/environments* and /v1/me/model-source.
type Handler struct {
	Envs  Environments
	Users UserPrefs
	Cfg   *config.Config
	Dial  SandboxDialer
	// BrowserProfiles records the resolved browser source for a hub key so the
	// browser hub attaches to the same source. nil = the hub uses config.
	BrowserProfiles func(userID, key string)

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
	// The subtree pattern also redirects /live to /live/ (query preserved).
	mux.HandleFunc(liveRoutePrefix+"/", h.live)
}

func (h *Handler) getModelSource(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	src := user.ModelSource
	if src == "" {
		src = storage.ModelSourceGateway
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"modelSource": src})
}

func (h *Handler) putModelSource(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		ModelSource string `json:"modelSource"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	src := strings.TrimSpace(body.ModelSource)
	if !storage.ValidModelSource(src) {
		httpx.WriteErr(w, http.StatusBadRequest, "modelSource must be gateway or own")
		return
	}
	if h.Users == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "user store not configured")
		return
	}
	if err := h.Users.SetModelSource(r.Context(), user.Username, src); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
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
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	list, err := h.Envs.List(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"environments": list})
}

func (h *Handler) ensureBrowser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	target, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if h.BrowserProfiles != nil {
		h.BrowserProfiles(user.Username, target.Key)
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
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) ensureAgent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	sb, err := h.Envs.EnsureAgent(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"slot":      userenv.SlotAgent,
		"sandboxId": sb.ID,
		"status":    sb.Status,
		"name":      sb.Name,
	})
}

func (h *Handler) upgradeAgent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	var body struct {
		Force bool `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := h.Envs.UpgradeAgent(r.Context(), user.Username, body.Force)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":      res.Status,
		"image":       res.Image,
		"digest":      res.Digest,
		"environment": res.Environment,
	})
}
