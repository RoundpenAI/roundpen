// Package httpapi serves Roundpen-native control-plane REST endpoints.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Handler is the native admin / ops API (distinct from E2B compatibility).
type Handler struct {
	Manager sandbox.Manager
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
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		writeErr(w, http.StatusInternalServerError, err.Error())
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
