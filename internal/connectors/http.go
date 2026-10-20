package connectors

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
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Handler serves personal Google connector routes.
type Handler struct {
	Svc *Service
}

// Mount registers authenticated me routes and the public Google callback.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/connectors", h.list)
	mux.HandleFunc("GET /v1/me/connectors/google/client", h.getClient)
	mux.HandleFunc("PUT /v1/me/connectors/google/client", h.putClient)
	mux.HandleFunc("DELETE /v1/me/connectors/google/client", h.deleteClient)
	mux.HandleFunc("POST /v1/me/connectors/google/connect", h.connect)
	mux.HandleFunc("DELETE /v1/me/connectors/google", h.disconnect)
	mux.HandleFunc("GET /v1/connectors/google/callback", h.callback)
	mux.HandleFunc("GET /v1/connectors/google/cloud/callback", h.cloudCallback)
}

type connectorStatusView struct {
	Kind      string `json:"kind"`
	Connected bool   `json:"connected"`
	HasClient bool   `json:"hasClient"`
	// Official is true when this install can connect through the shared relay
	// without a user-owned Google client.
	Official bool   `json:"official"`
	Email    string `json:"email,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Scopes   string `json:"scopes,omitempty"`
}

type clientView struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	CallbackURL  string `json:"callbackUrl"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	hasClient, grant, err := h.Svc.Status(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	view := connectorStatusView{Kind: KindGoogle, HasClient: hasClient, Official: h.Svc.Official()}
	if grant != nil {
		view.Connected = true
		view.Email = grant.Email
		view.Subject = grant.Subject
		view.Scopes = grant.Scopes
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"connectors": []connectorStatusView{view}})
}

func (h *Handler) getClient(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	c, err := h.Svc.Store.GetClient(r.Context(), user.Username)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			httpx.WriteJSON(w, http.StatusOK, clientView{
				CallbackURL: CallbackURL(httpx.DefaultTrust.Scheme(r), httpx.DefaultTrust.Host(r)),
			})
			return
		}
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, clientView{
		ClientID:     c.ClientID,
		ClientSecret: settings.MaskSecret(c.ClientSecret),
		CallbackURL:  CallbackURL(httpx.DefaultTrust.Scheme(r), httpx.DefaultTrust.Host(r)),
		UpdatedAt:    c.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) putClient(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var payload struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	clientID := strings.TrimSpace(payload.ClientID)
	if clientID == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "clientId is required")
		return
	}
	secret := strings.TrimSpace(payload.ClientSecret)
	if settings.IsSecretMask(secret) || secret == "" {
		existing, err := h.Svc.Store.GetClient(r.Context(), user.Username)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) || secret == "" {
				httpx.WriteErr(w, http.StatusBadRequest, "clientSecret is required")
				return
			}
			httpx.WriteErrOrInternal(w, r, err, nil)
			return
		}
		secret = existing.ClientSecret
	}
	c, err := h.Svc.SaveClient(r.Context(), user.Username, clientID, secret)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, clientView{
		ClientID:     c.ClientID,
		ClientSecret: settings.MaskSecret(c.ClientSecret),
		CallbackURL:  CallbackURL(httpx.DefaultTrust.Scheme(r), httpx.DefaultTrust.Host(r)),
		UpdatedAt:    c.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) deleteClient(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := h.Svc.DeleteClient(r.Context(), user.Username); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var payload struct {
		Return string `json:"return"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	scheme, host, err := httpx.ConsoleOrigin(r, payload.Return)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := h.Svc.StartConnect(r.Context(), user.Username, scheme, host, httpx.ConsolePath("/settings/apps"))
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrNoClient) {
			code = http.StatusBadRequest
		}
		httpx.WriteErr(w, code, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"authorizeUrl": raw})
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := h.Svc.Disconnect(r.Context(), user.Username); err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	if e := r.URL.Query().Get("error"); e != "" {
		http.Redirect(w, r, connectorsErrorPath("denied"), http.StatusFound)
		return
	}
	redirectTo, err := h.Svc.HandleCallback(r.Context(),
		r.URL.Query().Get("code"),
		r.URL.Query().Get("state"),
	)
	if err != nil {
		slog.Warn("google connector callback", "err", err)
		target := redirectTo
		if target == "" {
			target = httpx.ConsolePath("/settings/apps")
		}
		http.Redirect(w, r, withQuery(target, "google", errorCode(err)), http.StatusFound)
		return
	}
	http.Redirect(w, r, withQuery(redirectTo, "google", "linked"), http.StatusFound)
}

func (h *Handler) cloudCallback(w http.ResponseWriter, r *http.Request) {
	if e := r.URL.Query().Get("error"); e != "" {
		http.Redirect(w, r, connectorsErrorPath("denied"), http.StatusFound)
		return
	}
	redirectTo, err := h.Svc.HandleCloudCallback(r.Context(),
		r.URL.Query().Get("ticket"),
		r.URL.Query().Get("state"),
	)
	if err != nil {
		slog.Warn("google cloud callback", "err", err)
		target := redirectTo
		if target == "" {
			target = httpx.ConsolePath("/settings/apps")
		}
		http.Redirect(w, r, withQuery(target, "google", errorCode(err)), http.StatusFound)
		return
	}
	http.Redirect(w, r, withQuery(redirectTo, "google", "linked"), http.StatusFound)
}

func connectorsErrorPath(code string) string {
	return withQuery(httpx.ConsolePath("/settings/apps"), "google", code)
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrStateInvalid):
		return "state"
	case errors.Is(err, ErrNoClient):
		return "client"
	case errors.Is(err, ErrMissingCode):
		return "denied"
	default:
		return "failed"
	}
}

func withQuery(path, key, val string) string {
	u, err := url.Parse(path)
	if err != nil {
		return httpx.ConsolePath("/settings/apps") + "?" + url.Values{key: {val}}.Encode()
	}
	q := u.Query()
	q.Set(key, val)
	u.RawQuery = q.Encode()
	return u.RequestURI()
}
