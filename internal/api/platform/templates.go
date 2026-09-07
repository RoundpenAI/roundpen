package platform

import (
	"net/http"
	"time"

	"github.com/RoundpenAI/roundpen/internal/template"
)

type templateResp struct {
	TemplateID    string   `json:"templateID"`
	BuildID       string   `json:"buildID"`
	CPUCount      int      `json:"cpuCount"`
	MemoryMB      int      `json:"memoryMB"`
	DiskSizeMB    int      `json:"diskSizeMB"`
	Public        bool     `json:"public"`
	Profile       string   `json:"profile,omitempty"`
	Slot          string   `json:"slot,omitempty"`
	Aliases       []string `json:"aliases"`
	Names         []string `json:"names"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
	CreatedBy     *struct {
		ID    string  `json:"id"`
		Email *string `json:"email"`
	} `json:"createdBy"`
	LastSpawnedAt *string `json:"lastSpawnedAt"`
	SpawnCount    int64   `json:"spawnCount"`
	BuildCount    int     `json:"buildCount"`
	EnvdVersion   string  `json:"envdVersion"`
	BuildStatus   string  `json:"buildStatus"`
}

func toTemplateResp(rec template.Record) templateResp {
	var createdBy *struct {
		ID    string  `json:"id"`
		Email *string `json:"email"`
	}
	if rec.CreatedBy != "" {
		createdBy = &struct {
			ID    string  `json:"id"`
			Email *string `json:"email"`
		}{ID: rec.CreatedBy, Email: nil}
	}
	var lastSpawned *string
	if rec.LastSpawnedAt != nil {
		s := rec.LastSpawnedAt.UTC().Format(time.RFC3339)
		lastSpawned = &s
	}
	return templateResp{
		TemplateID:    rec.TemplateID,
		BuildID:       rec.BuildID,
		CPUCount:      rec.CPUCount,
		MemoryMB:      rec.MemoryMB,
		DiskSizeMB:    rec.DiskSizeMB,
		Public:        rec.Public,
		Profile:       rec.Profile,
		Slot:          rec.Slot,
		Aliases:       rec.Aliases,
		Names:         rec.Names,
		CreatedAt:     rec.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     rec.UpdatedAt.UTC().Format(time.RFC3339),
		CreatedBy:     createdBy,
		LastSpawnedAt: lastSpawned,
		SpawnCount:    rec.SpawnCount,
		BuildCount:    rec.BuildCount,
		EnvdVersion:   rec.EnvdVersion,
		BuildStatus:   string(rec.BuildStatus),
	}
}

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	if h.Templates == nil {
		writeJSON(w, http.StatusOK, []templateResp{})
		return
	}
	list, err := h.Templates.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]templateResp, 0, len(list))
	for _, rec := range list {
		out = append(out, toTemplateResp(rec))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) listTemplatesV2(w http.ResponseWriter, r *http.Request) {
	h.listTemplates(w, r)
}
