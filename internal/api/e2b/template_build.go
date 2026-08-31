package e2b

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/template"
)

type createTemplateV3Req struct {
	Name     string   `json:"name"`
	Alias    string   `json:"alias"`
	Tags     []string `json:"tags"`
	CPUCount int      `json:"cpuCount"`
	MemoryMB int      `json:"memoryMB"`
	Public   bool     `json:"public"`
}

type createTemplateV3Resp struct {
	TemplateID string   `json:"templateID"`
	BuildID    string   `json:"buildID"`
	Public     bool     `json:"public"`
	Names      []string `json:"names"`
	Tags       []string `json:"tags"`
	Aliases    []string `json:"aliases"`
}

type buildStatusResp struct {
	TemplateID string              `json:"templateID"`
	BuildID    string              `json:"buildID"`
	Status     string              `json:"status"`
	Logs       []string            `json:"logs"`
	LogEntries []buildLogEntryResp `json:"logEntries"`
	Reason     *buildReasonResp    `json:"reason,omitempty"`
}

type buildLogEntryResp struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Level     string `json:"level"`
	Step      string `json:"step,omitempty"`
}

type buildReasonResp struct {
	Message string `json:"message"`
}

func (h *Handler) createTemplateV3(w http.ResponseWriter, r *http.Request) {
	if h.Templates == nil {
		writeErr(w, http.StatusServiceUnavailable, "templates not configured")
		return
	}
	var req createTemplateV3Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(req.Alias)
	}
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	out, err := h.Templates.CreateTemplate(r.Context(), template.CreateTemplateRequest{
		Name:     name,
		Tags:     req.Tags,
		CPUCount: req.CPUCount,
		MemoryMB: req.MemoryMB,
		Public:   req.Public,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	display := template.DisplayName(out.Namespace, out.Name)
	writeJSON(w, http.StatusAccepted, createTemplateV3Resp{
		TemplateID: out.TemplateID,
		BuildID:    out.BuildID,
		Public:     out.Public,
		Names:      []string{display},
		Tags:       out.Tags,
		Aliases:    []string{display},
	})
}

func (h *Handler) startTemplateBuildV2(w http.ResponseWriter, r *http.Request) {
	if h.Templates == nil {
		writeErr(w, http.StatusServiceUnavailable, "templates not configured")
		return
	}
	templateID := r.PathValue("templateID")
	buildID := r.PathValue("buildID")
	var spec template.BuildSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.Templates.StartBuild(r.Context(), templateID, buildID, spec); err != nil {
		if errors.Is(err, template.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "not configured") {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{})
}

func (h *Handler) getTemplateBuildStatus(w http.ResponseWriter, r *http.Request) {
	if h.Templates == nil {
		writeErr(w, http.StatusServiceUnavailable, "templates not configured")
		return
	}
	templateID := r.PathValue("templateID")
	buildID := r.PathValue("buildID")
	offset, _ := strconv.Atoi(r.URL.Query().Get("logsOffset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	info, logs, err := h.Templates.GetBuildStatus(r.Context(), templateID, buildID, offset, limit)
	if errors.Is(err, template.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "build not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := buildStatusResp{
		TemplateID: templateID,
		BuildID:    buildID,
		Status:     string(info.Status),
		Logs:       make([]string, 0, len(logs)),
		LogEntries: make([]buildLogEntryResp, 0, len(logs)),
	}
	for _, e := range logs {
		resp.Logs = append(resp.Logs, e.Message)
		resp.LogEntries = append(resp.LogEntries, buildLogEntryResp{
			Timestamp: e.Timestamp,
			Message:   e.Message,
			Level:     e.Level,
			Step:      e.Step,
		})
	}
	if info.Status == template.BuildError && info.ErrorMessage != "" {
		resp.Reason = &buildReasonResp{Message: info.ErrorMessage}
	}
	writeJSON(w, http.StatusOK, resp)
}

// buildTemplate is a Roundpen convenience endpoint: create + start in one request.
func (h *Handler) buildTemplate(w http.ResponseWriter, r *http.Request) {
	if h.Templates == nil {
		writeErr(w, http.StatusServiceUnavailable, "templates not configured")
		return
	}
	var body struct {
		Name     string             `json:"name"`
		CPUCount int                `json:"cpuCount"`
		MemoryMB int                `json:"memoryMB"`
		Spec     template.BuildSpec `json:"spec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if body.Spec.CPUCount == 0 {
		body.Spec.CPUCount = body.CPUCount
	}
	if body.Spec.MemoryMB == 0 {
		body.Spec.MemoryMB = body.MemoryMB
	}
	created, err := h.Templates.CreateTemplate(r.Context(), template.CreateTemplateRequest{
		Name:     body.Name,
		CPUCount: body.Spec.CPUCount,
		MemoryMB: body.Spec.MemoryMB,
		Public:   true,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Templates.StartBuild(r.Context(), created.TemplateID, created.BuildID, body.Spec); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"templateID": created.TemplateID,
		"buildID":    created.BuildID,
		"name":       template.DisplayName(created.Namespace, created.Name),
	})
}
