// Package workspaceapi serves account-level Agent workspace file APIs.
package workspaceapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

const maxFileBytes = 50 << 20

// Environments ensures the caller's Agent slot.
type Environments interface {
	EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
}

// GuestFiles performs file ops inside a running Agent container.
type GuestFiles interface {
	ListGuestFiles(ctx context.Context, id, rel string) ([]workspace.DirEntry, error)
	ReadGuestFile(ctx context.Context, id, rel string) (io.ReadCloser, error)
	WriteGuestFile(ctx context.Context, id, rel string, r io.Reader) error
	RemoveGuestFile(ctx context.Context, id, rel string) error
	MoveGuestFile(ctx context.Context, id, src, dest string) error
	CopyGuestFile(ctx context.Context, id, src, dest string) error
}

// Handler serves /v1/me/workspace/files*.
type Handler struct {
	Envs  Environments
	Files GuestFiles
}

// Mount registers workspace routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/workspace/files", h.list)
	mux.HandleFunc("GET /v1/me/workspace/files/content", h.read)
	mux.HandleFunc("POST /v1/me/workspace/files", h.write)
	mux.HandleFunc("DELETE /v1/me/workspace/files", h.remove)
	mux.HandleFunc("POST /v1/me/workspace/files/move", h.move)
	mux.HandleFunc("POST /v1/me/workspace/files/copy", h.copy)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	sb, ok := h.ensure(w, r)
	if !ok {
		return
	}
	rel := filePath(r)
	entries, err := h.Files.ListGuestFiles(r.Context(), sb.ID, rel)
	if err != nil {
		writeGuestErr(w, err)
		return
	}
	if entries == nil {
		entries = []workspace.DirEntry{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"entries": entries, "path": rel})
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	sb, ok := h.ensure(w, r)
	if !ok {
		return
	}
	rel := filePath(r)
	if rel == "" || rel == "." {
		httpx.WriteErr(w, http.StatusBadRequest, "path is required")
		return
	}
	rc, err := h.Files.ReadGuestFile(r.Context(), sb.ID, rel)
	if err != nil {
		writeGuestErr(w, err)
		return
	}
	defer rc.Close()
	ct := inlineContentType(rel)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition",
			"attachment; filename*=UTF-8''"+url.PathEscape(path.Base(rel)))
	} else if ct != "application/octet-stream" {
		// Renderable types are shown inline; lock the response down so an
		// uploaded file can never script or fetch against the console origin.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// inlineContentType maps a workspace file onto the type browsers can show
// inline. HTML/SVG are deliberately text/plain: the console serves this from
// the same origin, so an uploaded file must never be treated as a document.
// Everything else stays application/octet-stream (download only).
func inlineContentType(rel string) string {
	switch strings.ToLower(path.Ext(rel)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".md", ".markdown", ".json", ".yaml", ".yml", ".toml", ".csv", ".log",
		".sh", ".bash", ".py", ".js", ".mjs", ".ts", ".tsx", ".jsx", ".css", ".go", ".rs",
		".java", ".c", ".h", ".cpp", ".rb", ".php", ".sql", ".html", ".htm", ".svg", ".xml":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request) {
	sb, ok := h.ensure(w, r)
	if !ok {
		return
	}
	rel := filePath(r)
	if rel == "" || rel == "." {
		httpx.WriteErr(w, http.StatusBadRequest, "path is required")
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxFileBytes)
	err := h.Files.WriteGuestFile(r.Context(), sb.ID, rel, body)
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") || strings.Contains(err.Error(), "file too large") {
			httpx.WriteErr(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
		writeGuestErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	sb, ok := h.ensure(w, r)
	if !ok {
		return
	}
	rel := filePath(r)
	if rel == "" || rel == "." {
		httpx.WriteErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := h.Files.RemoveGuestFile(r.Context(), sb.ID, rel); err != nil {
		writeGuestErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// relocateReq is the body of the move/copy endpoints; Path (query) is the
// source, Dest is either an existing directory or the literal target path.
type relocateReq struct {
	Dest string `json:"dest"`
}

func (h *Handler) move(w http.ResponseWriter, r *http.Request) {
	h.relocate(w, r, h.Files.MoveGuestFile)
}

func (h *Handler) copy(w http.ResponseWriter, r *http.Request) {
	h.relocate(w, r, h.Files.CopyGuestFile)
}

func (h *Handler) relocate(w http.ResponseWriter, r *http.Request, op func(ctx context.Context, id, src, dest string) error) {
	sb, ok := h.ensure(w, r)
	if !ok {
		return
	}
	src := filePath(r)
	if src == "" || src == "." {
		httpx.WriteErr(w, http.StatusBadRequest, "path is required")
		return
	}
	var req relocateReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	dest := strings.TrimSpace(req.Dest)
	if dest == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "dest is required")
		return
	}
	if err := op(r.Context(), sb.ID, src, dest); err != nil {
		writeGuestErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ensure(w http.ResponseWriter, r *http.Request) (*sandbox.Sandbox, bool) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	if h.Envs == nil || h.Files == nil {
		httpx.WriteErr(w, http.StatusServiceUnavailable, "workspace not configured")
		return nil, false
	}
	sb, err := h.Envs.EnsureAgent(r.Context(), user.Username)
	if err != nil {
		var nr *runtime.NotReady
		if errors.As(err, &nr) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, nr.Error())
			return nil, false
		}
		httpx.WriteErr(w, http.StatusServiceUnavailable, err.Error())
		return nil, false
	}
	return sb, true
}

func filePath(r *http.Request) string {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		return "."
	}
	return p
}

func writeGuestErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	if errors.Is(err, sandbox.ErrConflict) {
		httpx.WriteErr(w, http.StatusConflict, msg)
		return
	}
	if errors.Is(err, sandbox.ErrNotFound) || os.IsNotExist(err) || strings.Contains(msg, "no such file") {
		httpx.WriteErr(w, http.StatusNotFound, "path not found")
		return
	}
	if strings.Contains(msg, "escapes") || strings.Contains(msg, "path is required") {
		httpx.WriteErr(w, http.StatusBadRequest, msg)
		return
	}
	httpx.WriteErr(w, http.StatusBadRequest, msg)
}
