// Package platform serves Roundpen control-plane sandbox and template HTTP APIs.
package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/template"
)

// Handler serves sandbox lifecycle and template routes under /v1.
type Handler struct {
	Manager   sandbox.Manager
	Templates *template.Service
}

type newSandboxReq struct {
	Name       string            `json:"name"`
	Category   string            `json:"category"`
	IsDefault  bool              `json:"isDefault"`
	TemplateID string            `json:"templateID"`
	Timeout    int               `json:"timeout"` // seconds
	Metadata   map[string]string `json:"metadata"`
	EnvVars    map[string]string `json:"envVars"`
}

type sandboxResp struct {
	SandboxID   string            `json:"sandboxID"`
	Name        string            `json:"name,omitempty"`
	Category    string            `json:"category,omitempty"`
	IsDefault   bool              `json:"isDefault,omitempty"`
	TemplateID  string            `json:"templateID"`
	ClientID    string            `json:"clientID"`
	EnvdVersion string            `json:"envdVersion"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	State       string            `json:"state"`
	StartedAt   string            `json:"startedAt"`
	EndAt       string            `json:"endAt"`
	CPUCount    int               `json:"cpuCount"`
	MemoryMB    int               `json:"memoryMB"`
	DiskSizeMB  int               `json:"diskSizeMB"`
}

type timeoutReq struct {
	Timeout int `json:"timeout"`
}

type patchReq struct {
	Name      *string `json:"name"`
	Category  *string `json:"category"`
	IsDefault *bool   `json:"isDefault"`
}

// Mount registers Roundpen /v1 sandbox and template routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /v1/sandboxes", h.create)
	mux.HandleFunc("GET /v1/sandboxes", h.list)
	mux.HandleFunc("GET /v1/sandboxes/resolve", h.resolve)
	mux.HandleFunc("GET /v1/sandboxes/{sandboxID}", h.get)
	mux.HandleFunc("PATCH /v1/sandboxes/{sandboxID}", h.patch)
	mux.HandleFunc("DELETE /v1/sandboxes/{sandboxID}", h.delete)
	mux.HandleFunc("POST /v1/sandboxes/{sandboxID}/timeout", h.timeout)
	mux.HandleFunc("POST /v1/sandboxes/{sandboxID}/connect", h.connect)
	mux.HandleFunc("POST /v1/sandboxes/{sandboxID}/refreshes", h.refreshes)
	mux.HandleFunc("GET /v1/templates", h.listTemplates)
	mux.HandleFunc("GET /v1/templates/{templateID}", h.getTemplate)
	mux.HandleFunc("PATCH /v1/templates/{templateID}", h.patchTemplate)
	mux.HandleFunc("DELETE /v1/templates/{templateID}", h.deleteTemplate)
	mux.HandleFunc("POST /v1/templates", h.createTemplateV3)
	mux.HandleFunc("POST /v1/templates/{templateID}/builds", h.createTemplateBuildV2)
	mux.HandleFunc("POST /v1/templates/{templateID}/builds/{buildID}", h.startTemplateBuildV2)
	mux.HandleFunc("GET /v1/templates/{templateID}/builds/{buildID}/status", h.getTemplateBuildStatus)
	mux.HandleFunc("POST /v1/templates/build", h.buildTemplate)
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
		Name:       req.Name,
		Category:   req.Category,
		IsDefault:  req.IsDefault,
		TemplateID: req.TemplateID,
		TTL:        ttl,
		Env:        req.EnvVars,
		Metadata:   req.Metadata,
	})
	if errors.Is(err, sandbox.ErrConflict) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toResp(sb))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.Manager.List(r.Context(), sandbox.ListFilter{
		Category: r.URL.Query().Get("category"),
	})
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

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	sb, err := h.Manager.Resolve(r.Context(), sandbox.ResolveRequest{
		Name:     r.URL.Query().Get("name"),
		Category: r.URL.Query().Get("category"),
	})
	if errors.Is(err, sandbox.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toResp(sb))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sandboxID")
	sb, err := h.Manager.Get(r.Context(), id)
	if errors.Is(err, sandbox.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toResp(sb))
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sandboxID")
	var req patchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == nil && req.Category == nil && req.IsDefault == nil {
		writeErr(w, http.StatusBadRequest, "name, category, or isDefault required")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	sb, err := h.Manager.Update(r.Context(), id, sandbox.UpdateRequest{
		Name:      req.Name,
		Category:  req.Category,
		IsDefault: req.IsDefault,
	})
	if errors.Is(err, sandbox.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if errors.Is(err, sandbox.ErrConflict) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toResp(sb))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sandboxID")
	err := h.Manager.Delete(r.Context(), id)
	if errors.Is(err, sandbox.ErrNotFound) {
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
	if errors.Is(err, sandbox.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sandboxID")
	var body json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&body)

	sb, resumed, err := h.Manager.Connect(r.Context(), id)
	if errors.Is(err, sandbox.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "is failed") {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	code := http.StatusOK
	if resumed {
		code = http.StatusCreated
	}
	writeJSON(w, code, toResp(sb))
}

func (h *Handler) refreshes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sandboxID")
	_, err := h.Manager.Refresh(r.Context(), id)
	if errors.Is(err, sandbox.ErrNotFound) {
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
	startedAt := sb.CreatedAt.UTC().Format(time.RFC3339)
	endAt := startedAt
	if sb.ExpiresAt != nil {
		endAt = sb.ExpiresAt.UTC().Format(time.RFC3339)
	}
	cpuCount := sb.CPUCount
	if cpuCount == 0 {
		cpuCount = 1
	}
	memoryMB := sb.MemoryMB
	if memoryMB == 0 {
		memoryMB = 512
	}
	diskSizeMB := sb.DiskSizeMB
	if diskSizeMB == 0 {
		diskSizeMB = 5120
	}
	return sandboxResp{
		SandboxID:   sb.ID,
		Name:        sb.Name,
		Category:    sb.Category,
		IsDefault:   sb.IsDefault,
		TemplateID:  templateID,
		ClientID:    "roundpen",
		EnvdVersion: "0.0.0-roundpen",
		Metadata:    sb.Metadata,
		State:       sandboxState(sb.Status),
		StartedAt:   startedAt,
		EndAt:       endAt,
		CPUCount:    cpuCount,
		MemoryMB:    memoryMB,
		DiskSizeMB:  diskSizeMB,
	}
}

func sandboxState(st sandbox.Status) string {
	switch st {
	case sandbox.StatusRunning:
		return "running"
	case sandbox.StatusPaused:
		return "paused"
	case sandbox.StatusStopped:
		return "stopped"
	case sandbox.StatusFailed:
		return "failed"
	default:
		return string(st)
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
