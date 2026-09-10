// Package httpapi serves Roundpen-native control-plane REST endpoints.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

const maxFileBytes = 50 << 20 // 50 MiB

// Handler is the native admin / ops API for sandboxes.
type Handler struct {
	Manager   sandbox.Manager
	PublicURL string
}

type execReq struct {
	Command []string          `json:"command"`
	WorkDir string            `json:"workdir"`
	Env     map[string]string `json:"env"`
	Timeout int               `json:"timeout"` // seconds
}

type execResp struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// Mount registers native routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("POST /v1/sandboxes/{id}/exec", h.exec)
	mux.HandleFunc("POST /v1/sandboxes/{id}/stop", h.stop)

	mux.HandleFunc("GET /v1/sandboxes/{id}/files", h.listFiles)
	mux.HandleFunc("GET /v1/sandboxes/{id}/files/stat", h.statFile)
	mux.HandleFunc("GET /v1/sandboxes/{id}/files/content", h.readFile)
	mux.HandleFunc("POST /v1/sandboxes/{id}/files", h.writeFile)
	mux.HandleFunc("PUT /v1/sandboxes/{id}/files", h.writeFile)
	mux.HandleFunc("DELETE /v1/sandboxes/{id}/files", h.deleteFile)
}

func (h *Handler) exec(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req execReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Command) == 0 {
		writeErr(w, http.StatusBadRequest, "command is required")
		return
	}
	res, err := h.Manager.Exec(r.Context(), id, sandbox.ExecRequest{
		Cmd:     req.Command,
		WorkDir: req.WorkDir,
		Env:     req.Env,
		Timeout: time.Duration(req.Timeout) * time.Second,
	})
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, sandbox.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), " is stopped") || strings.Contains(err.Error(), " is paused") || strings.Contains(err.Error(), " is failed") {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, execResp{
		ExitCode: res.ExitCode,
		Stdout:   string(res.Stdout),
		Stderr:   string(res.Stderr),
	})
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := h.Manager.Stop(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func filePath(r *http.Request) string {
	p := r.URL.Query().Get("path")
	if p == "" {
		return "."
	}
	return p
}

func (h *Handler) listFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rel := filePath(r)
	entries, err := h.Manager.ListFiles(r.Context(), id, rel)
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "path not found")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if entries == nil {
		entries = []workspace.DirEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"path":    rel,
	})
}

func (h *Handler) statFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rel := filePath(r)
	st, err := h.Manager.StatFile(r.Context(), id, rel)
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, sandbox.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "path not found")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": rel,
		"stat": st,
	})
}

func (h *Handler) readFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, err := h.Manager.ReadFile(r.Context(), id, filePath(r))
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "path not found")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (h *Handler) writeFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path := filePath(r)
	if path == "" || path == "." {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxFileBytes)
	err := h.Manager.WriteFile(r.Context(), id, path, body)
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path := filePath(r)
	if path == "" || path == "." {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	err := h.Manager.RemoveFile(r.Context(), id, path)
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "path not found")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}
