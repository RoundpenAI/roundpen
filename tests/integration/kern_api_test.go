package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKernAPI_CreateExecDelete(t *testing.T) {
	h := startKernHarness(t)

	resp := h.mustDo(t, http.MethodGet, "/health", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes", map[string]any{
		"templateID": "host",
		"timeout":    600,
		"envVars":    map[string]string{"FOO": "bar"},
		"metadata":   map[string]string{"suite": "integration"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %s %s", resp.Status, b)
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	sid, _ := created["sandboxID"].(string)
	if sid == "" {
		t.Fatalf("missing sandboxID: %#v", created)
	}
	if created["state"] != "running" {
		t.Fatalf("state=%v", created["state"])
	}

	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+sid, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %s", resp.Status)
	}

	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes", nil)
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range list {
		if item["sandboxID"] == sid {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("sandbox %s not in list (%d items)", sid, len(list))
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/exec", map[string]any{
		"command": []string{"/bin/sh", "-c", "echo hello-$FOO > note.txt && cat note.txt"},
		"timeout": 30,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("exec: %s %s", resp.Status, b)
	}
	var execRes map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&execRes); err != nil {
		t.Fatal(err)
	}
	if int(execRes["exit_code"].(float64)) != 0 {
		t.Fatalf("exec result: %#v", execRes)
	}
	stdout, _ := execRes["stdout"].(string)
	if !strings.Contains(stdout, "hello-bar") {
		t.Fatalf("stdout=%q", stdout)
	}

	note := filepath.Join(h.DataRoot, "sandboxes", sid, "workspace", "note.txt")
	b, err := os.ReadFile(note)
	if err != nil {
		t.Fatalf("read workspace note: %v", err)
	}
	if string(b) != "hello-bar\n" {
		t.Fatalf("note.txt=%q", b)
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/timeout", map[string]any{"timeout": 900})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("timeout: %s %s", resp.Status, raw)
	}

	resp = h.mustDo(t, http.MethodDelete, "/v1/sandboxes/"+sid, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete: %s %s", resp.Status, raw)
	}

	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+sid, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %s", resp.Status)
	}
}

func TestKernAPI_ExecNotFound(t *testing.T) {
	h := startKernHarness(t)
	resp := h.mustDo(t, http.MethodPost, "/v1/sandboxes/does-not-exist/exec", map[string]any{
		"command": []string{"true"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %s", resp.Status)
	}
}

func TestKernAPI_StopThenExecFails(t *testing.T) {
	h := startKernHarness(t)
	resp := h.mustDo(t, http.MethodPost, "/v1/sandboxes", map[string]any{
		"templateID": "host",
		"timeout":    300,
	})
	defer resp.Body.Close()
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	sid := created["sandboxID"].(string)

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/stop", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("stop: %s %s", resp.Status, raw)
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/exec", map[string]any{
		"command": []string{"true"},
	})
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected exec on stopped sandbox to fail")
	}

	resp = h.mustDo(t, http.MethodDelete, "/v1/sandboxes/"+sid, nil)
	defer resp.Body.Close()
}

func (h *harness) mustDo(t testing.TB, method, path string, body any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.APIKey != "" {
		req.Header.Set("X-API-Key", h.APIKey)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
