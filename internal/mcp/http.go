package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/audit"
	"github.com/RoundpenAI/roundpen/internal/httpx"
)

// Handler serves personal MCP connector routes.
type Handler struct {
	Svc *Service
}

// Mount registers authenticated me routes and the public OAuth callback.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/mcp/catalog", h.catalog)
	mux.HandleFunc("GET /v1/me/mcp/connections", h.listConnections)
	mux.HandleFunc("POST /v1/me/mcp/connections", h.connect)
	mux.HandleFunc("DELETE /v1/me/mcp/connections/{serverId}", h.disconnect)
	mux.HandleFunc("POST /v1/me/mcp/connections/{serverId}/refresh-tools", h.refreshTools)
	mux.HandleFunc("POST /v1/me/mcp/connections/{serverId}/oauth/start", h.oauthStart)
	mux.HandleFunc("GET /v1/connectors/mcp/callback", h.callback)
}

type connectionView struct {
	ServerID      string `json:"serverId"`
	ServerName    string `json:"serverName"`
	AuthKind      string `json:"authKind"`
	Status        string `json:"status"`
	AccountLabel  string `json:"accountLabel,omitempty"`
	Scopes        string `json:"scopes,omitempty"`
	ToolCount     int    `json:"toolCount"`
	ToolsSyncedAt string `json:"toolsSyncedAt,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

type toolView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (h *Handler) view(c Connection) connectionView {
	name := c.ServerID
	if sv, ok := Lookup(c.ServerID); ok {
		name = sv.Name
	}
	v := connectionView{
		ServerID:     c.ServerID,
		ServerName:   name,
		AuthKind:     c.AuthKind,
		Status:       c.Status,
		AccountLabel: c.AccountLabel,
		Scopes:       c.Scopes,
		ToolCount:    len(c.Tools),
		LastError:    c.LastError,
	}
	if c.ToolsSyncedAt != nil {
		v.ToolsSyncedAt = c.ToolsSyncedAt.UTC().Format(time.RFC3339)
	}
	return v
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.Svc.Catalog(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"servers": items})
}

func (h *Handler) listConnections(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	conns, err := h.Svc.Store.ListConnections(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	views := make([]connectionView, 0, len(conns))
	for _, c := range conns {
		views = append(views, h.view(c))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"connections": views})
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var payload struct {
		ServerID string `json:"serverId"`
		APIKey   string `json:"apiKey"`
	}
	if err := readJSON(r, &payload); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	conn, err := h.Svc.ConnectAPIKey(r.Context(), user.Username, payload.ServerID, payload.APIKey)
	if err != nil {
		writeConnectError(w, r, err)
		return
	}
	audit.Record(r.Context(), "mcp.connect", "server", conn.ServerID, "auth", conn.AuthKind)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"connection": h.view(*conn)})
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	serverID := r.PathValue("serverId")
	if err := h.Svc.Disconnect(r.Context(), user.Username, serverID); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	audit.Record(r.Context(), "mcp.disconnect", "server", serverID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) refreshTools(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	serverID := r.PathValue("serverId")
	conn, err := h.Svc.RefreshTools(r.Context(), user.Username, serverID)
	if err != nil {
		writeConnectError(w, r, err)
		return
	}
	audit.Record(r.Context(), "mcp.tools.refresh", "server", serverID, "tools", len(conn.Tools))
	tools := make([]toolView, 0, len(conn.Tools))
	for _, t := range conn.Tools {
		tools = append(tools, toolView{Name: t.Name, Description: t.Description})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"connection": h.view(*conn),
		"tools":      tools,
	})
}

func (h *Handler) oauthStart(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	serverID := r.PathValue("serverId")
	var payload struct {
		RedirectTo string `json:"redirectTo"`
	}
	if r.Body != nil {
		_ = readJSON(r, &payload) // an empty body is fine
	}
	raw, err := h.Svc.StartOAuth(r.Context(), user.Username, serverID,
		httpx.DefaultTrust.Scheme(r), httpx.DefaultTrust.Host(r), payload.RedirectTo)
	if err != nil {
		writeConnectError(w, r, err)
		return
	}
	audit.Record(r.Context(), "mcp.connect", "server", serverID, "auth", "oauth")
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"authorizeUrl": raw})
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectTo, serverID, err := h.Svc.HandleCallback(r.Context(), q.Get("code"), q.Get("state"), q.Get("error"))
	if redirectTo == "" {
		redirectTo = "/settings/mcp"
	}
	if err != nil {
		slog.Warn("mcp connector callback", "err", err, "server", serverID)
		target := withQuery(redirectTo, "mcpResult", "failed")
		target = withQuery(target, "mcpError", callbackErrorCode(err))
		if serverID != "" {
			target = withQuery(target, "mcp", serverID)
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	target := withQuery(redirectTo, "mcpResult", "linked")
	if serverID != "" {
		target = withQuery(target, "mcp", serverID)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func callbackErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrStateInvalid):
		return "state"
	case errors.Is(err, ErrDenied), errors.Is(err, ErrMissingCode):
		return "denied"
	case errors.Is(err, ErrDCRUnsupported):
		return "dcr"
	case errors.Is(err, ErrNotSupported), errors.Is(err, ErrServerUnknown):
		return "unsupported"
	}
	var te *TokenError
	if errors.As(err, &te) {
		return "token"
	}
	return "failed"
}

func writeConnectError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrServerUnknown):
		httpx.WriteErr(w, http.StatusNotFound, "unknown MCP server")
	case errors.Is(err, ErrNotSupported):
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotConnected):
		httpx.WriteErr(w, http.StatusNotFound, err.Error())
	default:
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
	}
}

func readJSON(r *http.Request, dest any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func withQuery(path, key, val string) string {
	u, err := url.Parse(path)
	if err != nil {
		return "/settings/mcp?" + url.Values{key: {val}}.Encode()
	}
	q := u.Query()
	q.Set(key, val)
	u.RawQuery = q.Encode()
	return u.RequestURI()
}
