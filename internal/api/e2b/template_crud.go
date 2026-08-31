package e2b

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/RoundpenAI/roundpen/internal/template"
)

type templateDetailResp struct {
	templateResp
	Builtin   bool              `json:"builtin"`
	Builds    []buildSummaryResp `json:"builds"`
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Description string          `json:"description"`
	Profile   string            `json:"profile"`
}

type buildSummaryResp struct {
	BuildID      string `json:"buildID"`
	Status       string `json:"status"`
	ArtifactRef  string `json:"artifactRef,omitempty"`
	CPUCount     int    `json:"cpuCount"`
	MemoryMB     int    `json:"memoryMB"`
	DiskSizeMB   int    `json:"diskSizeMB"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type patchTemplateReq struct {
	Description *string `json:"description"`
	Public      *bool   `json:"public"`
	CPUCount    *int    `json:"cpuCount"`
	MemoryMB    *int    `json:"memoryMB"`
	DiskSizeMB  *int    `json:"diskSizeMB"`
}

func toTemplateDetailResp(d template.TemplateDetail) templateDetailResp {
	out := templateDetailResp{
		templateResp: toTemplateResp(d.Record),
		Builtin:      d.Builtin,
		Namespace:    d.Namespace,
		Name:         d.Name,
		Description:  d.Description,
		Profile:      d.Profile,
		Builds:       make([]buildSummaryResp, 0, len(d.Builds)),
	}
	for _, b := range d.Builds {
		out.Builds = append(out.Builds, buildSummaryResp{
			BuildID:      b.BuildID,
			Status:       string(b.Status),
			ArtifactRef:  b.ArtifactRef,
			CPUCount:     b.CPUCount,
			MemoryMB:     b.MemoryMB,
			DiskSizeMB:   b.DiskSizeMB,
			ErrorMessage: b.ErrorMessage,
			CreatedAt:    b.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:    b.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	if h.Templates == nil {
		writeErr(w, http.StatusServiceUnavailable, "templates not configured")
		return
	}
	id := r.PathValue("templateID")
	detail, err := h.Templates.Get(r.Context(), id)
	if errors.Is(err, template.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, toTemplateDetailResp(detail))
}

func (h *Handler) patchTemplate(w http.ResponseWriter, r *http.Request) {
	if h.Templates == nil {
		writeErr(w, http.StatusServiceUnavailable, "templates not configured")
		return
	}
	id := r.PathValue("templateID")
	var req patchTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rec, err := h.Templates.Update(r.Context(), id, template.UpdateTemplateRequest{
		Description: req.Description,
		Public:      req.Public,
		CPUCount:    req.CPUCount,
		MemoryMB:    req.MemoryMB,
		DiskSizeMB:  req.DiskSizeMB,
	})
	if errors.Is(err, template.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "template not found")
		return
	}
	if errors.Is(err, template.ErrBuiltin) {
		writeErr(w, http.StatusForbidden, "built-in template is read-only")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toTemplateResp(rec))
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	if h.Templates == nil {
		writeErr(w, http.StatusServiceUnavailable, "templates not configured")
		return
	}
	id := r.PathValue("templateID")
	err := h.Templates.Delete(r.Context(), id)
	if errors.Is(err, template.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "template not found")
		return
	}
	if errors.Is(err, template.ErrBuiltin) {
		writeErr(w, http.StatusForbidden, "built-in template cannot be deleted")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
