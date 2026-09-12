// Package workspaceapi serves account-level Agent workspace file APIs.
package workspaceapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
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
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "path": rel})
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	sb, ok := h.ensure(w, r)
	if !ok {
		return
	}
	rel := filePath(r)
	if rel == "" || rel == "." {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	rc, err := h.Files.ReadGuestFile(r.Context(), sb.ID, rel)
	if err != nil {
		writeGuestErr(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request) {
	sb, ok := h.ensure(w, r)
	if !ok {
		return
	}
	rel := filePath(r)
	if rel == "" || rel == "." {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxFileBytes)
	err := h.Files.WriteGuestFile(r.Context(), sb.ID, rel, body)
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") || strings.Contains(err.Error(), "file too large") {
			writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
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
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := h.Files.RemoveGuestFile(r.Context(), sb.ID, rel); err != nil {
		writeGuestErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ensure(w http.ResponseWriter, r *http.Request) (*sandbox.Sandbox, bool) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	if h.Envs == nil || h.Files == nil {
		writeErr(w, http.StatusServiceUnavailable, "workspace not configured")
		return nil, false
	}
	sb, err := h.Envs.EnsureAgent(r.Context(), user.Username)
	if err != nil {
		var nr *runtime.NotReady
		if errors.As(err, &nr) {
			writeErr(w, http.StatusServiceUnavailable, nr.Error())
			return nil, false
		}
		writeErr(w, http.StatusServiceUnavailable, err.Error())
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
	if errors.Is(err, sandbox.ErrNotFound) || os.IsNotExist(err) || strings.Contains(msg, "no such file") {
		writeErr(w, http.StatusNotFound, "path not found")
		return
	}
	if strings.Contains(msg, "escapes") || strings.Contains(msg, "path is required") {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	writeErr(w, http.StatusBadRequest, msg)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
