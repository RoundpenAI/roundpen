package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func TestListDirectoryGrants(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterReadImage(reg, &tools.AgentBinder{Host: &fakeHost{}})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "ListDirectoryGrants", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/vol1/share") {
		t.Fatalf("grants missing path: %s", out)
	}
}

func TestReadImageHostGranted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	host := &fakeHost{content: string(raw)}
	// fakeHost returns content bytes; OpenHost ignores path — OK for unit test.
	reg := tools.NewRegistry()
	tools.RegisterReadImage(reg, &tools.AgentBinder{Host: host})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "ReadImage",
		json.RawMessage(`{"file_path":"/vol1/share/shot.png"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload: %s", out)
	}
	vis, _ := payload[tools.VisionPayloadKey].(map[string]any)
	if vis == nil || vis["base64"] == nil {
		t.Fatalf("missing vision payload: %s", out)
	}
	summary, mime, b64, ok := tools.StripVisionPayload("ReadImage", out)
	if !ok || mime == "" || b64 == "" {
		t.Fatalf("strip failed: ok=%v mime=%q", ok, mime)
	}
	if strings.Contains(summary, `"base64"`) {
		t.Fatalf("summary still has base64: %s", summary)
	}
}

func TestReadImageDeniedWithoutHost(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterReadImage(reg, &tools.AgentBinder{})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "ReadImage",
		json.RawMessage(`{"file_path":"/vol1/share/a.png"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("want host unavailable, got %v", err)
	}
}

func TestReadRejectsImageBytes(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	host := &fakeHost{content: buf.String()}
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Host: host})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Read",
		json.RawMessage(`{"file_path":"/vol1/share/a.png"}`))
	if err == nil || !strings.Contains(err.Error(), "ReadImage") {
		t.Fatalf("want ReadImage hint, got %v", err)
	}
}

// Ensure fakeHost still satisfies HostFiles after ListGrants was added.
var _ tools.HostFiles = (*fakeHost)(nil)

func TestStripVisionPayloadIgnoresOtherTools(t *testing.T) {
	_, _, _, ok := tools.StripVisionPayload("Read", `{"ok":true}`)
	if ok {
		t.Fatal("expected false")
	}
	_ = io.Discard
}
