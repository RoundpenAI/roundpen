package oauth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Handler serves the OAuth2 HTTP surface: public login routes, the signed-in
// identity manager and the admin provider configuration.
type Handler struct {
	Svc      *Service
	Sessions storage.SessionStore
}

// Mount registers every route. The public ones are whitelisted in
// internal/api/auth/middleware.go.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/auth/oauth/providers", h.listEnabled)
	mux.HandleFunc("GET /v1/auth/oauth/{provider}/start", h.start)
	mux.HandleFunc("GET /v1/auth/oauth/{provider}/callback", h.callback)
	// The official relay sends the browser here. The path is fixed in the worker.
	mux.HandleFunc("GET /v1/auth/oauth/github/cloud/callback", h.cloudCallback)
	// GitHub App Manifest: GitHub redirects the admin's browser here after
	// they create the app. Public like other OAuth callbacks; the state row
	// and the admin session bind the handshake.
	mux.HandleFunc("GET /v1/auth/oauth/github/manifest/callback", h.githubManifestCallback)

	mux.HandleFunc("GET /v1/me/identities", h.listIdentities)
	mux.HandleFunc("GET /v1/me/oauth/providers", h.listLinkable)
	mux.HandleFunc("POST /v1/me/identities/link/{provider}", h.link)
	mux.HandleFunc("DELETE /v1/me/identities/{id}", h.unlink)

	mux.HandleFunc("GET /v1/admin/oauth/providers", auth.RequireAdmin(h.adminList))
	mux.HandleFunc("PUT /v1/admin/oauth/providers", auth.RequireAdmin(h.adminSave))
	mux.HandleFunc("DELETE /v1/admin/oauth/providers/{id}", auth.RequireAdmin(h.adminDelete))
	mux.HandleFunc("GET /v1/admin/oauth/github/manifest", auth.RequireAdmin(h.githubManifestStart))
}

type providerView struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Host  string `json:"host"`
	Label string `json:"label"`
}

type adminProviderView struct {
	Provider
	// DisplayLabel is what the UI shows; CallbackURL is what the admin has to
	// register on the remote platform (derived from the request, never stored).
	DisplayLabel string `json:"displayLabel"`
	CallbackURL  string `json:"callbackUrl"`
}

type adminListView struct {
	Providers []adminProviderView `json:"providers"`
	Features  adminFeatures       `json:"features"`
}

type adminFeatures struct {
	SelfHostedGit bool `json:"selfHostedGit"`
}

func (h *Handler) listEnabled(w http.ResponseWriter, r *http.Request) {
	providers, err := h.Svc.ListEnabledProviders(r.Context())
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	out := make([]providerView, 0, len(providers))
	for _, p := range providers {
		out = append(out, providerView{ID: p.ID, Kind: p.Kind, Host: p.Host, Label: p.DisplayLabel()})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	scheme, host, err := httpx.ConsoleOrigin(r, r.URL.Query().Get("return"))
	if err != nil {
		slog.Warn("oauth start origin", "provider", r.PathValue("provider"), "err", err)
		http.Redirect(w, r, loginErrorPath("", "failed"), http.StatusFound)
		return
	}
	raw, err := h.Svc.AuthorizeURL(r.Context(), StartInput{
		ProviderID:    r.PathValue("provider"),
		RedirectTo:    r.URL.Query().Get("redirect"),
		ConsoleScheme: scheme,
		ConsoleHost:   host,
	})
	if err != nil {
		slog.Warn("oauth start", "provider", r.PathValue("provider"), "err", err)
		http.Redirect(w, r, loginErrorPath("", errorCode(err)), http.StatusFound)
		return
	}
	http.Redirect(w, r, raw, http.StatusFound)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	sessionUser := ""
	if user := auth.GetUser(r.Context()); user != nil {
		sessionUser = user.Username
	}
	if e := r.URL.Query().Get("error"); e != "" {
		http.Redirect(w, r, loginErrorPath(sessionUser, "denied"), http.StatusFound)
		return
	}
	res, err := h.Svc.HandleCallback(r.Context(), CallbackInput{
		ProviderID:  providerID,
		Code:        r.URL.Query().Get("code"),
		State:       r.URL.Query().Get("state"),
		SessionUser: sessionUser,
	})
	if err != nil {
		slog.Warn("oauth callback", "provider", providerID, "err", err)
		http.Redirect(w, r, loginErrorPath(sessionUser, errorCode(err)), http.StatusFound)
		return
	}
	if _, err := auth.IssueSession(w, r, h.Sessions, res.User); err != nil {
		slog.Error("oauth session", "user", res.User.Username, "err", err)
		http.Redirect(w, r, loginErrorPath(sessionUser, "failed"), http.StatusFound)
		return
	}
	target := res.RedirectTo
	if res.Linked {
		target = withQuery(target, "oauth_linked", "1")
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *Handler) cloudCallback(w http.ResponseWriter, r *http.Request) {
	sessionUser := ""
	if user := auth.GetUser(r.Context()); user != nil {
		sessionUser = user.Username
	}
	res, err := h.Svc.HandleCloudCallback(r.Context(), CloudCallbackInput{
		Ticket:      r.URL.Query().Get("ticket"),
		State:       r.URL.Query().Get("state"),
		Error:       r.URL.Query().Get("error"),
		SessionUser: sessionUser,
	})
	if err != nil {
		slog.Warn("github cloud callback", "err", err)
		http.Redirect(w, r, loginErrorPath(sessionUser, errorCode(err)), http.StatusFound)
		return
	}
	if _, err := auth.IssueSession(w, r, h.Sessions, res.User); err != nil {
		slog.Error("github cloud session", "user", res.User.Username, "err", err)
		http.Redirect(w, r, loginErrorPath(sessionUser, "failed"), http.StatusFound)
		return
	}
	target := res.RedirectTo
	if res.Linked {
		target = withQuery(target, "oauth_linked", "1")
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var payload struct {
		Return string `json:"return"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload)
	}
	scheme, host, err := httpx.ConsoleOrigin(r, payload.Return)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := h.Svc.AuthorizeURL(r.Context(), StartInput{
		ProviderID:    r.PathValue("provider"),
		LinkUser:      user.Username,
		RedirectTo:    "/settings/apps",
		ConsoleScheme: scheme,
		ConsoleHost:   host,
	})
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"authorizeUrl": raw})
}

type identityView struct {
	ID              string     `json:"id"`
	ProviderID      string     `json:"providerId"`
	ProviderKind    string     `json:"providerKind"`
	ProviderLabel   string     `json:"providerLabel"`
	ProviderHost    string     `json:"providerHost"`
	Login           string     `json:"login"`
	Name            string     `json:"name"`
	Email           string     `json:"email"`
	Scopes          string     `json:"scopes"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	HasRefreshToken bool       `json:"hasRefreshToken"`
	LastLoginAt     *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

func (h *Handler) listLinkable(w http.ResponseWriter, r *http.Request) {
	if auth.GetUser(r.Context()) == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	providers, err := h.Svc.ListLinkProviders(r.Context())
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	out := make([]providerView, 0, len(providers))
	for _, p := range providers {
		out = append(out, providerView{ID: p.ID, Kind: p.Kind, Host: p.Host, Label: p.DisplayLabel()})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (h *Handler) listIdentities(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	idents, err := h.Svc.Identities(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	out := make([]identityView, 0, len(idents))
	for _, id := range idents {
		label := id.ProviderLabel
		if label == "" {
			label = defaultLabel(id.ProviderKind)
		}
		out = append(out, identityView{
			ID: id.ID, ProviderID: id.ProviderID, ProviderKind: id.ProviderKind,
			ProviderLabel: label, ProviderHost: id.ProviderHost,
			Login: id.Login, Name: id.Name, Email: id.Email, Scopes: id.Scopes,
			ExpiresAt: id.TokenExpiresAt, HasRefreshToken: id.RefreshToken != "",
			LastLoginAt: id.LastLoginAt, CreatedAt: id.CreatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"identities": out})
}

func (h *Handler) unlink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	err := h.Svc.Unlink(r.Context(), user.Username, r.PathValue("id"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case isNotFound(err):
		httpx.WriteErr(w, http.StatusNotFound, "identity not found")
	default:
		httpx.WriteErrOrInternal(w, r, err, nil)
	}
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	providers, err := h.Svc.ListProviders(r.Context())
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, adminListView{
		Providers: h.adminViews(r, providers),
		Features:  adminFeatures{SelfHostedGit: h.Svc.selfHostedAllowed()},
	})
}

func (h *Handler) adminSave(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var payload Provider
	if err := json.Unmarshal(raw, &payload); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A masked/empty secret means "keep the stored one".
	if prev, err := h.Svc.Provider(r.Context(), payload.ID); err == nil {
		payload.ClientSecret = settings.ResolveSecret(payload.ClientSecret, prev.ClientSecret)
	} else if settings.IsSecretMask(payload.ClientSecret) {
		payload.ClientSecret = ""
	}
	saved, err := h.Svc.SaveProvider(r.Context(), payload)
	if err != nil {
		if errors.Is(err, ErrSelfHostedPluginDisabled) {
			httpx.WriteErr(w, http.StatusConflict, err.Error())
			return
		}
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	views := h.adminViews(r, []Provider{*saved})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider": views[0]})
}

func (h *Handler) githubManifestStart(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	out, err := h.Svc.StartGitHubManifest(r.Context(), user.Username,
		httpx.DefaultTrust.Scheme(r), httpx.DefaultTrust.Host(r))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) githubManifestCallback(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	sessionUser := ""
	if user != nil {
		sessionUser = user.Username
	}
	if e := r.URL.Query().Get("error"); e != "" {
		http.Redirect(w, r, githubSettingsError("denied"), http.StatusFound)
		return
	}
	if user == nil || user.Role != storage.RoleAdmin {
		http.Redirect(w, r, githubSettingsError("session"), http.StatusFound)
		return
	}
	_, err := h.Svc.CompleteGitHubManifest(r.Context(),
		r.URL.Query().Get("code"), r.URL.Query().Get("state"), sessionUser)
	if err != nil {
		slog.Warn("github manifest callback", "err", err)
		http.Redirect(w, r, githubSettingsError(githubManifestErrorCode(err)), http.StatusFound)
		return
	}
	http.Redirect(w, r, githubSettingsPath+"?github_app=1", http.StatusFound)
}

func githubManifestErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrStateInvalid):
		return "state"
	case errors.Is(err, ErrStateUserMismatch):
		return "session"
	default:
		return "failed"
	}
}

func githubSettingsError(code string) string {
	return withQuery(githubSettingsPath, "github_app_error", code)
}

func (h *Handler) adminDelete(w http.ResponseWriter, r *http.Request) {
	err := h.Svc.DeleteProvider(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case isNotFound(err):
		httpx.WriteErr(w, http.StatusNotFound, "provider not found")
	default:
		httpx.WriteErrOrInternal(w, r, err, nil)
	}
}

func (h *Handler) adminViews(r *http.Request, providers []Provider) []adminProviderView {
	scheme, host := httpx.DefaultTrust.Scheme(r), httpx.DefaultTrust.Host(r)
	out := make([]adminProviderView, 0, len(providers))
	for _, p := range providers {
		masked := p
		masked.ClientSecret = settings.MaskSecret(p.ClientSecret)
		out = append(out, adminProviderView{
			Provider:     masked,
			DisplayLabel: p.DisplayLabel(),
			CallbackURL:  p.CallbackURL(scheme, host),
		})
	}
	return out
}

// errorCode maps a flow error to the short code shown on the login page.
func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrStateInvalid):
		return "state"
	case errors.Is(err, ErrStateUserMismatch):
		return "session"
	case errors.Is(err, ErrRegistrationDisabled):
		return "registration_disabled"
	case errors.Is(err, ErrAlreadyLinked), errors.Is(err, ErrLinkedElsewhere):
		return "already_linked"
	case errors.Is(err, ErrProviderUnavailable):
		return "provider"
	default:
		return "failed"
	}
}

// loginErrorPath sends anonymous failures to the login page and signed-in ones
// back to the settings page that started a link flow.
func loginErrorPath(sessionUser, code string) string {
	if sessionUser != "" {
		return withQuery("/settings/apps", "oauth_error", code)
	}
	return withQuery("/login", "oauth_error", code)
}

func withQuery(path, key, value string) string {
	return path + "?" + key + "=" + value
}

func isNotFound(err error) bool { return errors.Is(err, storage.ErrNotFound) }
