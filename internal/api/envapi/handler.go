// Package envapi exposes fixed per-user environments (agent / browser / mobile).
package envapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// VNCSockLookup resolves a sandbox id to a QEMU VNC unix socket path.
type VNCSockLookup interface {
	VNCSock(sandboxID string) (string, error)
}

// Environments is the userenv surface used by environment HTTP handlers.
type Environments interface {
	List(ctx context.Context, userID string) ([]userenv.EnvView, error)
	EnsureBrowser(ctx context.Context, userID string) (*sandbox.Sandbox, error)
	EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
}

// Handler serves /v1/me/environments*.
type Handler struct {
	Envs      Environments
	Prefs     *runtime.PrefStore
	Tokens    *preview.Store
	PublicURL string
	VNC       VNCSockLookup
}

var desktopUpgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	Subprotocols: []string{"binary"},
}

// Mount registers environment routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/environments", h.list)
	mux.HandleFunc("POST /v1/me/environments/browser/ensure", h.ensureBrowser)
	mux.HandleFunc("POST /v1/me/environments/agent/ensure", h.ensureAgent)
	mux.HandleFunc("GET /v1/me/environments/browser/desktop", h.desktopLink)
	mux.HandleFunc("GET /v1/me/environments/browser/desktop/ws", h.desktopWS)
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
	sb, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slot":      userenv.SlotBrowser,
		"sandboxId": sb.ID,
		"status":    sb.Status,
		"name":      sb.Name,
	})
}

type ensureAgentReq struct {
	Engine string `json:"engine"`
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
	var req ensureAgentReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	if eng := runtime.NormalizeEngine(req.Engine); eng != "" && h.Prefs != nil {
		if err := h.Prefs.SetAgentEngine(r.Context(), user.Username, eng); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
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

func (h *Handler) desktopLink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil || h.Tokens == nil {
		writeErr(w, http.StatusServiceUnavailable, "desktop not configured")
		return
	}
	sb, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	token, exp, err := h.Tokens.Issue(sb.ID, 0, user.Username) // port 0 = desktop/VNC
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	base := strings.TrimRight(h.PublicURL, "/")
	if base == "" {
		base = "http://" + r.Host
	}
	wsURL := fmt.Sprintf("%s/v1/me/environments/browser/desktop/ws?token=%s", httpToWS(base), token)
	writeJSON(w, http.StatusOK, map[string]any{
		"sandboxId": sb.ID,
		"wsUrl":     wsURL,
		"token":     token,
		"expiresAt": exp.UTC().Format(time.RFC3339),
	})
}

func httpToWS(u string) string {
	u = strings.TrimSpace(u)
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	default:
		return u
	}
}

func (h *Handler) desktopWS(w http.ResponseWriter, r *http.Request) {
	if h.Tokens == nil || h.VNC == nil {
		http.Error(w, "desktop not configured", http.StatusServiceUnavailable)
		return
	}
	token := r.URL.Query().Get("token")
	sandboxID, _, _, ok := h.Tokens.Lookup(token)
	if !ok || sandboxID == "" {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	sock, err := h.VNC.VNCSock(sandboxID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	unixConn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err != nil {
		http.Error(w, "vnc dial: "+err.Error(), http.StatusBadGateway)
		return
	}
	ws, err := desktopUpgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = unixConn.Close()
		return
	}
	defer ws.Close()
	defer unixConn.Close()

	errCh := make(chan error, 2)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := unixConn.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					errCh <- werr
					return
				}
			}
			if err != nil {
				errCh <- err
				return
			}
		}
	}()
	go func() {
		for {
			mt, data, err := ws.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
				continue
			}
			if _, err := unixConn.Write(data); err != nil {
				errCh <- err
				return
			}
		}
	}()
	<-errCh
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
