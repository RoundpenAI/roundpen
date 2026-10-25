package gitcred

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/vcs"
)

// Handler serves /v1/me/git-credentials.
type Handler struct {
	Store *Store
	// Identities, when set, supplies OAuth-derived tokens (e.g. connected GitHub).
	Identities interface {
		CredsForUser(ctx context.Context, userID string) ([]Cred, error)
	}
	HTTP *http.Client
	// OnChange, when set, is called with the local user after a credential is
	// written or removed, so a running sandbox picks the change up without
	// waiting for a token refresh or a rebuild.
	OnChange func(ctx context.Context, userID string)
}

func (h *Handler) changed(ctx context.Context, userID string) {
	if h.OnChange != nil {
		h.OnChange(ctx, userID)
	}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/git-credentials", h.list)
	mux.HandleFunc("PUT /v1/me/git-credentials", h.upsert)
	mux.HandleFunc("DELETE /v1/me/git-credentials/{id}", h.delete)
	mux.HandleFunc("GET /v1/me/git-repos", h.listRepos)
}

func (h *Handler) mergedCreds(ctx context.Context, userID string) ([]Cred, error) {
	var pats []Cred
	if h.Store != nil {
		list, err := h.Store.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		pats = list
	}
	var idents []Cred
	if h.Identities != nil {
		list, err := h.Identities.CredsForUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		idents = list
	}
	return MergeCreds(pats, idents), nil
}

func (h *Handler) githubToken(ctx context.Context, userID string) string {
	creds, err := h.mergedCreds(ctx, userID)
	if err != nil {
		return ""
	}
	for _, c := range creds {
		if strings.TrimSpace(c.Token) == "" {
			continue
		}
		if c.Provider == ProviderGitHub || c.Host == "github.com" {
			return c.Token
		}
	}
	return ""
}

// listRepos returns remote repositories for a connected git host (GitHub first).
func (h *Handler) listRepos(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	host := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("host")))
	if host == "" {
		host = "github.com"
	}
	if host != "github.com" {
		httpx.WriteErr(w, http.StatusBadRequest, "only github.com is supported for repo listing")
		return
	}
	token := h.githubToken(r.Context(), user.Username)
	if token == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"connected": false,
			"repos":     []vcs.RemoteRepo{},
		})
		return
	}
	client := h.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	repos, err := vcs.ListGitHubRepos(r.Context(), client, token)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"connected": true,
		"host":      host,
		"repos":     repos,
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"credentials": []Cred{}})
		return
	}
	list, err := h.Store.List(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	out := make([]Cred, 0, len(list))
	for _, c := range list {
		out = append(out, c.SanitizeForResponse())
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "git credentials not configured")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var in UpsertInput
	if err := json.Unmarshal(raw, &in); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.Store.Upsert(r.Context(), user.Username, in)
	if err != nil {
		if strings.Contains(err.Error(), "required") {
			httpx.WriteErr(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	h.changed(r.Context(), user.Username)
	httpx.WriteJSON(w, http.StatusOK, c.SanitizeForResponse())
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.Store.Delete(r.Context(), user.Username, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteErr(w, http.StatusNotFound, "not found")
			return
		}
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	h.changed(r.Context(), user.Username)
	w.WriteHeader(http.StatusNoContent)
}
