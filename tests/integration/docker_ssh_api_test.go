package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestDockerSSHAPI_CreateExecDelete(t *testing.T) {
	h := startDockerSSHHarness(t)

	resp := h.mustDo(t, http.MethodPost, "/sandboxes", map[string]any{
		"templateID": "alpine:3.20",
		"timeout":    600,
		"metadata":   map[string]string{"suite": "docker-ssh"},
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
	sid := created["sandboxID"].(string)

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/exec", map[string]any{
		"command": []string{"sh", "-c", "echo ok > /workspace/it.txt && cat /workspace/it.txt"},
		"timeout": 60,
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
		t.Fatalf("exec: %#v", execRes)
	}

	resp = h.mustDo(t, http.MethodDelete, "/sandboxes/"+sid, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete: %s %s", resp.Status, b)
	}
}
