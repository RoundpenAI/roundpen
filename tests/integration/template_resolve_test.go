package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestTemplateResolveSandboxResources(t *testing.T) {
	h := startKernHarness(t)

	resp := h.mustDo(t, http.MethodPost, "/sandboxes", map[string]any{
		"templateID": "python",
		"timeout":    300,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create sandbox: %s body=%s", resp.Status, b)
	}

	var sb map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		t.Fatal(err)
	}
	if sb["templateID"] != "python" {
		t.Fatalf("templateID=%v", sb["templateID"])
	}
	cpu, _ := sb["cpuCount"].(float64)
	mem, _ := sb["memoryMB"].(float64)
	if cpu != 1 || mem != 1024 {
		t.Fatalf("resources cpu=%v mem=%v", cpu, mem)
	}

	id, _ := sb["sandboxID"].(string)
	if id == "" {
		t.Fatal("empty sandboxID")
	}
	t.Cleanup(func() {
		del := h.mustDo(t, http.MethodDelete, "/sandboxes/"+id, nil)
		del.Body.Close()
	})
}

func TestTemplateListIncludesSeeded(t *testing.T) {
	h := startKernHarness(t)

	resp := h.mustDo(t, http.MethodGet, "/templates", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("list templates: %s body=%s", resp.Status, b)
	}

	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) < 5 {
		t.Fatalf("expected seeded templates, got %d", len(list))
	}
	found := false
	for _, rec := range list {
		for _, alias := range rec["aliases"].([]any) {
			if alias == "python" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("python template not listed")
	}
}
