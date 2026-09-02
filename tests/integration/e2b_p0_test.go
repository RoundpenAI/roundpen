package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestE2B_P0ConnectRefreshesAndSchema(t *testing.T) {
	h := startKernHarness(t)

	resp := h.mustDo(t, http.MethodPost, "/sandboxes", map[string]any{
		"templateID": "host",
		"timeout":    600,
	})
	defer resp.Body.Close()
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	sid := created["sandboxID"].(string)

	for _, field := range []string{"startedAt", "endAt", "cpuCount", "memoryMB", "diskSizeMB", "state"} {
		if created[field] == nil || created[field] == "" {
			t.Fatalf("create missing %s in %#v", field, created)
		}
	}

	resp = h.mustDo(t, http.MethodPost, "/sandboxes/"+sid+"/connect", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("connect running: %s %s", resp.Status, b)
	}

	resp = h.mustDo(t, http.MethodPost, "/sandboxes/"+sid+"/refreshes", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("refreshes: %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/stop", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("stop: %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/exec", map[string]any{"command": []string{"true"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("exec on stopped want 409, got %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+sid+"/files/stat?path=.", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("stat: %s %s", resp.Status, b)
	}

	resp = h.mustDo(t, http.MethodPost, "/sandboxes/"+sid+"/connect", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("connect resume: %s %s", resp.Status, b)
	}

	req, err := http.NewRequest(http.MethodPost, h.URL+"/v1/sandboxes/"+sid+"/files?path=resume.txt", strings.NewReader("ok"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", h.APIKey)
	resp, err = h.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("write after resume: %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodDelete, "/sandboxes/"+sid, nil)
	defer resp.Body.Close()
}
