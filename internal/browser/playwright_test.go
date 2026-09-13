// internal/browser/playwright_test.go
package browser

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestPlaywrightEngineAgainstRealBrowser exercises the engine against a real
// CDP endpoint. Set ROUNDPEN_TEST_BROWSER_WS (e.g.
// ws://10.10.1.3:3000/chrome) to run; skipped otherwise.
func TestPlaywrightEngineAgainstRealBrowser(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("ROUNDPEN_TEST_BROWSER_WS"))
	if endpoint == "" {
		t.Skip("ROUNDPEN_TEST_BROWSER_WS not set")
	}
	d := &pwDriver{}
	defer d.stop()
	eng, err := newPlaywrightEngine(d, pwAttach{Endpoint: endpoint, Width: 1280, Height: 800})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	probe, err := ProbeCDP(ctx, endpoint, "")
	if err != nil || probe.Path == "" {
		t.Fatalf("probe %s: %+v, %v", endpoint, probe, err)
	}
	if probe.Version == "" {
		t.Log("endpoint has no /meta version (plain Chrome CDP endpoint)")
	}
	if err := eng.SetViewport(ctx, 1024, 768); err != nil {
		t.Fatalf("set viewport: %v", err)
	}
	// data: URL keeps the test independent of network access from the browser.
	html := "data:text/html,<html><head><title>engine</title></head><body>" +
		"<input placeholder=q><button onclick=\"document.title='clicked'\">Go</button></body></html>"
	if err := eng.Navigate(ctx, html); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	snap, err := eng.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Width != 1024 || snap.Height != 768 {
		t.Fatalf("snapshot viewport = %dx%d, want 1024x768", snap.Width, snap.Height)
	}
	var inputRef, buttonRef string
	for _, n := range snap.Nodes {
		switch n.Tag {
		case "input":
			inputRef = n.Ref
		case "button":
			buttonRef = n.Ref
		}
	}
	if inputRef == "" || buttonRef == "" {
		t.Fatalf("refs missing in snapshot: %+v", snap.Nodes)
	}
	if err := eng.Type(ctx, inputRef, "hello", false); err != nil {
		t.Fatalf("type: %v", err)
	}
	v, err := eng.Evaluate(ctx, "document.querySelector('input').value")
	if err != nil {
		t.Fatalf("evaluate input value: %v", err)
	}
	var value string
	if err := json.Unmarshal(v, &value); err != nil {
		t.Fatalf("unmarshal input value %s: %v", v, err)
	}
	if value != "hello" {
		t.Fatalf("input value = %q, want hello", value)
	}
	if err := eng.Type(ctx, inputRef, "x", true); err != nil {
		t.Fatalf("type submit: %v", err)
	}
	if err := eng.Hover(ctx, buttonRef); err != nil {
		t.Fatalf("hover: %v", err)
	}
	if err := eng.Click(ctx, buttonRef); err != nil {
		t.Fatalf("click: %v", err)
	}
	if title, err := eng.Title(ctx); err != nil || title != "clicked" {
		t.Fatalf("title = %q, %v (want clicked)", title, err)
	}
	if _, err := eng.Evaluate(ctx, "1+1"); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	png, err := eng.Screenshot(ctx)
	if err != nil || len(png) < 1000 {
		t.Fatalf("screenshot: %d bytes, %v", len(png), err)
	}
	if err := eng.InputClick(ctx, 20, 20); err != nil {
		t.Fatalf("input click: %v", err)
	}
	if err := eng.InputType(ctx, "x"); err != nil {
		t.Fatalf("input type: %v", err)
	}
	if err := eng.InputKey(ctx, "Tab"); err != nil {
		t.Fatalf("input key: %v", err)
	}
	if err := eng.Press(ctx, "Enter"); err != nil {
		t.Fatalf("press: %v", err)
	}
}
