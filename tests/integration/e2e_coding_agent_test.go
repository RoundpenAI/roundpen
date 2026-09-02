// End-to-end coding-agent workflow against the integration harness (Kern + PG).
package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestE2E_CodingAgentWorkflow exercises the full agent loop:
// create → connect → exec → files → stop/resume → delete.
func TestE2E_CodingAgentWorkflow(t *testing.T) {
	h := startKernHarness(t)
	sid := e2eCreateSandbox(t, h, map[string]any{
		"templateID": "host",
		"timeout":    900,
		"envVars":    map[string]string{"AGENT": "e2e"},
		"metadata":   map[string]string{"agent": "coding-e2e"},
	})
	t.Cleanup(func() {
		resp := h.mustDo(t, http.MethodDelete, "/sandboxes/"+sid, nil)
		resp.Body.Close()
	})

	// 1. Connect (E2B SDK entry)
	resp := h.mustDo(t, http.MethodPost, "/sandboxes/"+sid+"/connect", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect: %s", resp.Status)
	}

	// 2. Exec: env var + write project file
	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/exec", map[string]any{
		"command": []string{"/bin/sh", "-c", "echo agent-$AGENT > main.py && python3 --version || echo no-python"},
		"timeout": 30,
	})
	defer resp.Body.Close()
	var execOut map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&execOut); err != nil {
		t.Fatal(err)
	}
	if int(execOut["exit_code"].(float64)) != 0 {
		t.Fatalf("exec failed: %#v", execOut)
	}

	// 3. Upload README via files API
	req, err := http.NewRequest(http.MethodPost, h.URL+"/v1/sandboxes/"+sid+"/files?path=README.md", strings.NewReader("# e2e\n"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", h.APIKey)
	resp, err = h.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("write README: %s", resp.Status)
	}

	// 4. List + stat + read content
	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+sid+"/files?path=.", nil)
	defer resp.Body.Close()
	var listed struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if !e2eHasFile(listed.Entries, "main.py") || !e2eHasFile(listed.Entries, "README.md") {
		t.Fatalf("missing files in listing: %#v", listed.Entries)
	}

	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+sid+"/files/stat?path=README.md", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stat: %s", resp.Status)
	}

	req, err = http.NewRequest(http.MethodGet, h.URL+"/v1/sandboxes/"+sid+"/files/content?path=main.py", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", h.APIKey)
	resp, err = h.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "agent-e2e") {
		t.Fatalf("main.py content=%q", body)
	}

	// 5. Refresh TTL
	resp = h.mustDo(t, http.MethodPost, "/sandboxes/"+sid+"/refreshes", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("refreshes: %s", resp.Status)
	}

	// 6. Stop: exec blocked, files still OK
	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/stop", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("stop: %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/exec", map[string]any{"command": []string{"true"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("exec when stopped want 409, got %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+sid+"/files/content?path=README.md", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read while stopped: %s", resp.Status)
	}

	// 7. Connect resume + exec again
	resp = h.mustDo(t, http.MethodPost, "/sandboxes/"+sid+"/connect", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("connect resume: %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/exec", map[string]any{
		"command": []string{"/bin/sh", "-c", "echo resumed >> main.py"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("exec after resume: %s %s", resp.Status, b)
	}

	// 8. Preview link minting
	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+sid+"/preview-link?port=8080", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview-link: %s", resp.Status)
	}

	t.Logf("e2e coding agent workflow OK sandbox=%s", sid)
}

func e2eCreateSandbox(t *testing.T, h *harness, body map[string]any) string {
	t.Helper()
	resp := h.mustDo(t, http.MethodPost, "/sandboxes", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %s %s", resp.Status, b)
	}
	var sb map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		t.Fatal(err)
	}
	sid, _ := sb["sandboxID"].(string)
	if sid == "" {
		t.Fatalf("no sandboxID in %#v", sb)
	}
	return sid
}

func e2eHasFile(entries []map[string]any, name string) bool {
	for _, e := range entries {
		if e["name"] == name {
			return true
		}
	}
	return false
}
