package userworkspace

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/fnosfile"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/userenv"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// Recreator rebuilds the agent sandbox so a new mount takes effect.
type Recreator interface {
	RecreateAgent(ctx context.Context, userID string) (*userenv.UpgradeResult, error)
}

// Handler serves the per-user folder binding.
type Handler struct {
	Store    *Store
	Files    *fnosfile.Client
	Envs     Recreator
	DataRoot string
}

// Mount registers /v1/me/workspace-root.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/workspace-root", h.get)
	mux.HandleFunc("PUT /v1/me/workspace-root", h.put)
	mux.HandleFunc("DELETE /v1/me/workspace-root", h.del)
}

type view struct {
	Enabled      bool   `json:"enabled"`
	Bound        bool   `json:"bound"`
	DisplayPath  string `json:"displayPath,omitempty"`
	RebuildError string `json:"rebuildError,omitempty"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.view(r.Context(), user.Username, language(r)))
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Files == nil || !h.Files.Enabled() {
		httpx.WriteErr(w, http.StatusNotFound, "folder binding is only available on fnOS")
		return
	}
	uid, ok := FnosUID(user.AuthProvider, user.Username)
	if !ok {
		httpx.WriteErr(w, http.StatusForbidden, "only an fnOS account can bind a folder")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	path, err := AcceptPath(body.Path)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	folders, err := h.Files.UserFolders(ctx, uid)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if !Covers(folders, path) {
		httpx.WriteErr(w, http.StatusForbidden, "that folder is not authorized for this app")
		return
	}
	acl, err := h.Files.CheckACL(ctx, uid, path)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if !acl.Writable {
		httpx.WriteErr(w, http.StatusForbidden, "you cannot write that folder")
		return
	}
	private := ""
	if h.DataRoot != "" {
		private = filepath.Join(h.DataRoot, "sandboxes", workspace.UserWorkspaceID(user.Username), "workspace")
	}
	if err := Prepare(path, private); err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := h.Store.Put(ctx, user.Username, path); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := h.view(ctx, user.Username, language(r))
	if h.Envs != nil {
		if _, err := h.Envs.RecreateAgent(ctx, user.Username); err != nil {
			out.RebuildError = err.Error()
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := h.Store.Delete(r.Context(), user.Username); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := h.view(r.Context(), user.Username, language(r))
	if h.Envs != nil {
		if _, err := h.Envs.RecreateAgent(r.Context(), user.Username); err != nil {
			out.RebuildError = err.Error()
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) view(ctx context.Context, userID, lang string) view {
	out := view{Enabled: h.Files != nil && h.Files.Enabled()}
	if h.Store == nil {
		return out
	}
	path, err := h.Store.Get(ctx, userID)
	if err != nil || path == "" {
		return out
	}
	out.Bound = true
	out.DisplayPath = filepath.Base(path)
	if h.Files != nil && h.Files.Enabled() {
		if label, err := h.Files.DisplayPath(ctx, path, lang); err == nil && label != "" {
			out.DisplayPath = label
		}
	}
	return out
}

func language(r *http.Request) string {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "en") {
		return "en-US"
	}
	return "zh-CN"
}
