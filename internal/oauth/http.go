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

	mux.HandleFunc("GET /v1/me/identities", h.listIdentities)
	mux.HandleFunc("POST /v1/me/identities/link/{provider}", h.link)
	mux.HandleFunc("DELETE /v1/me/identities/{id}", h.unlink)

	mux.HandleFunc("GET /v1/admin/oauth/providers", auth.RequireAdmin(h.adminList))
	mux.HandleFunc("PUT /v1/admin/oauth/providers", auth.RequireAdmin(h.adminSave))
	mux.HandleFunc("DELETE /v1/admin/oauth/providers/{id}", auth.RequireAdmin(h.adminDelete))
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

func (h *Handler) listEnabled(w http.ResponseWriter, r *http.Request) {
	providers, err := h.Svc.ListEnabledProviders(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]providerView, 0, len(providers))
	for _, p := range providers {
		out = append(out, providerView{ID: p.ID, Kind: p.Kind, Host: p.Host, Label: p.DisplayLabel()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	raw, err := h.Svc.AuthorizeURL(r.Context(), StartInput{
		ProviderID:    r.PathValue("provider"),
		RedirectTo:    r.URL.Query().Get("redirect"),
		ConsoleScheme: httpx.DefaultTrust.Scheme(r),
		ConsoleHost:   httpx.DefaultTrust.Host(r),
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

func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	raw, err := h.Svc.AuthorizeURL(r.Context(), StartInput{
		ProviderID:    r.PathValue("provider"),
		LinkUser:      user.Username,
		RedirectTo:    "/settings/accounts",
		ConsoleScheme: httpx.DefaultTrust.Scheme(r),
		ConsoleHost:   httpx.DefaultTrust.Host(r),
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorizeUrl": raw})
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

func (h *Handler) listIdentities(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	idents, err := h.Svc.Identities(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
	writeJSON(w, http.StatusOK, map[string]any{"identities": out})
}

func (h *Handler) unlink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	err := h.Svc.Unlink(r.Context(), user.Username, r.PathValue("id"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case isNotFound(err):
		writeErr(w, http.StatusNotFound, "identity not found")
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	providers, err := h.Svc.ListProviders(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": h.adminViews(r, providers)})
}

func (h *Handler) adminSave(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var payload Provider
	if err := json.Unmarshal(raw, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
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
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	views := h.adminViews(r, []Provider{*saved})
	writeJSON(w, http.StatusOK, map[string]any{"provider": views[0]})
}

func (h *Handler) adminDelete(w http.ResponseWriter, r *http.Request) {
	err := h.Svc.DeleteProvider(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case isNotFound(err):
		writeErr(w, http.StatusNotFound, "provider not found")
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		return withQuery("/settings/accounts", "oauth_error", code)
	}
	return withQuery("/login", "oauth_error", code)
}

func withQuery(path, key, value string) string {
	return path + "?" + key + "=" + value
}

func isNotFound(err error) bool { return errors.Is(err, storage.ErrNotFound) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}
