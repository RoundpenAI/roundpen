package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/workspace/sshfs"
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

	// Control plane writes into remote workspace via SSH FS.
	if err := h.FS.Write(context.Background(), sid, "from-control.txt", bytes.NewReader([]byte("ctrl\n"))); err != nil {
		t.Fatalf("control-plane write: %v", err)
	}

	resp = h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+sid+"/exec", map[string]any{
		"command": []string{"sh", "-c", "cat /workspace/from-control.txt && echo ok > /workspace/from-sandbox.txt && cat /workspace/from-sandbox.txt"},
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
	stdout, _ := execRes["stdout"].(string)
	if !strings.Contains(stdout, "ctrl") || !strings.Contains(stdout, "ok") {
		t.Fatalf("stdout=%q", stdout)
	}

	rc, err := h.FS.Open(context.Background(), sid, "from-sandbox.txt")
	if err != nil {
		t.Fatalf("control-plane open: %v", err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "ok\n" {
		t.Fatalf("remote file=%q", b)
	}

	resp = h.mustDo(t, http.MethodDelete, "/sandboxes/"+sid, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete: %s %s", resp.Status, raw)
	}
}

func TestSSHFS_LiveRemote(t *testing.T) {
	host := os.Getenv("ROUNDPEN_TEST_DOCKER_HOST")
	if host == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_HOST")
	}
	root := os.Getenv("ROUNDPEN_TEST_REMOTE_ROOT")
	if root == "" {
		root = "/tmp/roundpen-it"
	}
	fs, err := sshfs.NewFromDockerHost(host, root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := "sshfs-live"
	if _, err := fs.Create(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Remove(context.Background(), id) })
	if err := fs.Write(ctx, id, "x.txt", bytes.NewReader([]byte("live"))); err != nil {
		t.Fatal(err)
	}
	rc, err := fs.Open(ctx, id, "x.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "live" {
		t.Fatalf("got %q", b)
	}
}
