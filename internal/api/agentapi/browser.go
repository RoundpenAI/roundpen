package agentapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

func (h *Handler) mountBrowser(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agent-sessions/{id}/browser", h.browserStatus)
	mux.HandleFunc("GET /v1/agent-sessions/{id}/browser/screenshot", h.browserScreenshot)
	mux.HandleFunc("POST /v1/agent-sessions/{id}/browser/takeover", h.browserTakeover)
	mux.HandleFunc("POST /v1/agent-sessions/{id}/browser/input", h.browserInput)
}

func (h *Handler) loadOwnedSession(w http.ResponseWriter, r *http.Request) (*agentsession.Session, bool) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	id := r.PathValue("id")
	sess, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, agentsession.ErrNotFound) {
		httpx.WriteErr(w, http.StatusNotFound, "not found")
		return nil, false
	}
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return nil, false
	}
	if sess.UserID != user.Username && user.Role != "admin" {
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
		return nil, false
	}
	return sess, true
}

// hubKey resolves the browser Hub id without starting the Browser VM.
// Status polls must stay cheap; use ensureHubKey when CDP work is needed.
func (h *Handler) hubKey(r *http.Request, sess *agentsession.Session) string {
	if h.Envs != nil && sess != nil {
		userID := sess.UserID
		if user := auth.GetUser(r.Context()); user != nil {
			userID = user.Username
		}
		// Ask the provider first: a stale sandbox mapping must not shadow the
		// key EnsureBrowser hands out for external providers.
		if h.Envs.Provider() != config.CDPProviderDocker {
			return userenv.BrowserKey(userID)
		}
		if id, err := h.Envs.BrowserSandboxID(r.Context(), userID); err == nil && id != "" {
			return id
		}
	}
	return browser.AgentBrowserID(sess.ID)
}

// ensureHubKey starts/resumes the user's Browser environment when present,
// then returns the Hub id used for CDP.
func (h *Handler) ensureHubKey(r *http.Request, sess *agentsession.Session) string {
	if h.Envs != nil && sess != nil {
		userID := sess.UserID
		if user := auth.GetUser(r.Context()); user != nil {
			userID = user.Username
		}
		if target, err := h.Envs.EnsureBrowser(r.Context(), userID); err == nil && target != nil {
			if h.Hub != nil {
				h.Hub.SetProfile(target.Key, h.Envs.BrowserProfileFor(userID))
			}
			return target.Key
		}
		// EnsureBrowser failed: only the docker provider has a managed container
		// to fall back to. External providers keep their stable user key.
		if h.Envs.Provider() != config.CDPProviderDocker {
			return userenv.BrowserKey(userID)
		}
		if id, err := h.Envs.BrowserSandboxID(r.Context(), userID); err == nil && id != "" {
			return id
		}
	}
	return browser.AgentBrowserID(sess.ID)
}

func (h *Handler) browserStatus(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.loadOwnedSession(w, r)
	if !ok {
		return
	}
	if h.Hub == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "browser hub not configured")
		return
	}
	key := h.hubKey(r, sess)
	st := h.Hub.StatusEx(key)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"sessionId": sess.ID,
		"hubId":     key,
		"attached":  st.Attached,
		"url":       st.URL,
		"width":     st.Width,
		"height":    st.Height,
		"takeover":  st.Takeover,
	})
}

func (h *Handler) browserScreenshot(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.loadOwnedSession(w, r)
	if !ok {
		return
	}
	if h.Hub == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "browser hub not configured")
		return
	}
	bs, err := h.Hub.Ensure(r.Context(), h.ensureHubKey(r, sess))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	png, err := bs.Engine.Screenshot(r.Context())
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

type takeoverReq struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) browserTakeover(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.loadOwnedSession(w, r)
	if !ok {
		return
	}
	if h.Hub == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "browser hub not configured")
		return
	}
	var req takeoverReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	key := h.ensureHubKey(r, sess)
	if req.Enabled {
		bs, err := h.Hub.Ensure(r.Context(), key)
		if err != nil {
			httpx.WriteErr(w, http.StatusBadGateway, err.Error())
			return
		}
		// Ensure there is a real page frame to screenshot (fresh Chrome is blank).
		if strings.TrimSpace(bs.Engine.URL()) == "" {
			_ = bs.Engine.Navigate(r.Context(), "about:blank")
		}
	}
	if err := h.Hub.SetTakeover(key, req.Enabled); err != nil {
		httpx.WriteErr(w, http.StatusConflict, err.Error())
		return
	}
	st := h.Hub.StatusEx(key)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"attached": st.Attached,
		"takeover": st.Takeover,
		"url":      st.URL,
		"width":    st.Width,
		"height":   st.Height,
	})
}

type browserInputReq struct {
	Type   string  `json:"type"` // click | move | wheel | type | key
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	DeltaX float64 `json:"deltaX"`
	DeltaY float64 `json:"deltaY"`
	Text   string  `json:"text"`
	Key    string  `json:"key"`
}

func (h *Handler) browserInput(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.loadOwnedSession(w, r)
	if !ok {
		return
	}
	if h.Hub == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "browser hub not configured")
		return
	}
	key := h.ensureHubKey(r, sess)
	if !h.Hub.Takeover(key) {
		httpx.WriteErr(w, http.StatusConflict, "takeover not enabled")
		return
	}
	var req browserInputReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bs, err := h.Hub.Ensure(r.Context(), key)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	switch strings.ToLower(strings.TrimSpace(req.Type)) {
	case "click":
		err = bs.Engine.InputClick(r.Context(), req.X, req.Y)
	case "move":
		err = bs.Engine.InputMove(r.Context(), req.X, req.Y)
	case "wheel":
		err = bs.Engine.InputWheel(r.Context(), req.X, req.Y, req.DeltaX, req.DeltaY)
	case "type":
		if req.Text == "" {
			httpx.WriteErr(w, http.StatusBadRequest, "text is required")
			return
		}
		err = bs.Engine.InputType(r.Context(), req.Text)
	case "key":
		if strings.TrimSpace(req.Key) == "" {
			httpx.WriteErr(w, http.StatusBadRequest, "key is required")
			return
		}
		err = bs.Engine.InputKey(r.Context(), req.Key)
	default:
		httpx.WriteErr(w, http.StatusBadRequest, "type must be click|move|wheel|type|key")
		return
	}
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "url": bs.Engine.URL()})
}
