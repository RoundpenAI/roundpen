// Package envapi exposes fixed per-user environments (agent / browser / mobile).
package envapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
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
}

// Handler serves /v1/me/environments*.
type Handler struct {
	Envs Environments
	Cfg  *config.Config
	Dial SandboxDialer

	liveMu    sync.Mutex
	liveCache map[string]liveTarget // username -> resolved browser container
}

// Mount registers environment routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/environments", h.list)
	mux.HandleFunc("POST /v1/me/environments/browser/ensure", h.ensureBrowser)
	mux.HandleFunc("POST /v1/me/environments/agent/ensure", h.ensureAgent)
	mux.HandleFunc("GET /v1/me/environments/browser/live-link", h.liveLink)
	// The subtree pattern also redirects /live to /live/ (query preserved).
	mux.HandleFunc(liveRoutePrefix+"/", h.live)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
