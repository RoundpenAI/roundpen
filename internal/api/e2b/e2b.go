// Package e2b adapts Roundpen's Sandbox Manager to E2B-compatible platform routes.
package e2b

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Handler serves E2B-compatible routes.
type Handler struct {
	Manager sandbox.Manager
}

type newSandboxReq struct {
	TemplateID string            `json:"templateID"`
	Timeout    int               `json:"timeout"` // seconds
	Metadata   map[string]string `json:"metadata"`
	EnvVars    map[string]string `json:"envVars"`
}

type sandboxResp struct {
	SandboxID   string            `json:"sandboxID"`
	TemplateID  string            `json:"templateID"`
	ClientID    string            `json:"clientID"`
	EnvdVersion string            `json:"envdVersion"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	State       string            `json:"state,omitempty"`
}

type timeoutReq struct {
	Timeout int `json:"timeout"`
}

// Mount registers E2B routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /sandboxes", h.create)
	mux.HandleFunc("GET /sandboxes", h.list)
	mux.HandleFunc("GET /sandboxes/{sandboxID}", h.get)
	mux.HandleFunc("DELETE /sandboxes/{sandboxID}", h.delete)
	mux.HandleFunc("POST /sandboxes/{sandboxID}/timeout", h.timeout)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req newSandboxReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ttl := time.Duration(req.Timeout) * time.Second
	sb, err := h.Manager.Create(r.Context(), sandbox.CreateRequest{
		TemplateID: req.TemplateID,
		TTL:        ttl,
		Env:        req.EnvVars,
		Metadata:   req.Metadata,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toResp(sb))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.Manager.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]sandboxResp, 0, len(list))
	for _, sb := range list {
		out = append(out, toResp(sb))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sandboxID")
	sb, err := h.Manager.Get(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toResp(sb))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sandboxID")
	err := h.Manager.Delete(r.Context(), id)
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

func (h *Handler) timeout(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sandboxID")
	var req timeoutReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	_, err := h.Manager.SetTimeout(r.Context(), id, time.Duration(req.Timeout)*time.Second)
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

func toResp(sb *sandbox.Sandbox) sandboxResp {
	templateID := sb.Image
	if sb.Metadata != nil {
		if t, ok := sb.Metadata["templateID"]; ok && t != "" {
			templateID = t
		}
	}
	return sandboxResp{
		SandboxID:   sb.ID,
		TemplateID:  templateID,
		ClientID:    "roundpen",
		EnvdVersion: "0.0.0-roundpen",
		Metadata:    sb.Metadata,
		State:       string(sb.Status),
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}
