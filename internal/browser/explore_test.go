package browser

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

type scriptPage struct {
	url      string
	title    string
	nodes    []SnapNode
	revealed map[string][]SnapNode // hover ref → extra nodes
	dead     map[string]bool
	next     map[string]string // click ref → next url
}

type scriptEngine struct {
	mu      sync.Mutex
	pages   map[string]*scriptPage
	cur     string
	hovered string
	clicks  []string
	hovers  []string
}

func (e *scriptEngine) page() *scriptPage {
	return e.pages[e.cur]
}

func (e *scriptEngine) visible() []SnapNode {
	p := e.page()
	if p == nil {
		return nil
	}
	out := append([]SnapNode{}, p.nodes...)
	if extra := p.revealed[e.hovered]; extra != nil {
		out = append(out, extra...)
	}
	return out
}

func (e *scriptEngine) Navigate(_ context.Context, url string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cur = url
	e.hovered = ""
	return nil
}
func (e *scriptEngine) URL() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cur
}
func (e *scriptEngine) Title(context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p := e.page(); p != nil {
		return p.title, nil
	}
	return "", nil
}
func (e *scriptEngine) Snapshot(context.Context) (Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.page()
	if p == nil {
		return Snapshot{URL: e.cur}, nil
	}
	nodes := e.visible()
	return Snapshot{URL: p.url, Title: p.title, Nodes: nodes}, nil
}
func (e *scriptEngine) Hover(_ context.Context, ref string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hovered = ref
	e.hovers = append(e.hovers, ref)
	return nil
}
func (e *scriptEngine) Click(_ context.Context, ref string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clicks = append(e.clicks, ref)
	p := e.page()
	if p != nil && p.next[ref] != "" {
		e.cur = p.next[ref]
		e.hovered = ""
	}
	return nil
}
func (e *scriptEngine) Type(context.Context, string, string, bool) error { return nil }
func (e *scriptEngine) Press(context.Context, string) error              { return nil }
func (e *scriptEngine) Screenshot(context.Context) ([]byte, error)       { return nil, nil }
func (e *scriptEngine) SetViewport(context.Context, int, int) error      { return nil }
func (e *scriptEngine) Evaluate(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`[]`), nil
}
func (e *scriptEngine) InputClick(context.Context, float64, float64) error { return nil }
func (e *scriptEngine) InputMove(context.Context, float64, float64) error  { return nil }
func (e *scriptEngine) InputWheel(context.Context, float64, float64, float64, float64) error {
	return nil
}
func (e *scriptEngine) InputType(context.Context, string) error { return nil }
func (e *scriptEngine) InputKey(context.Context, string) error  { return nil }
func (e *scriptEngine) Close() error                            { return nil }

func TestExplore_hoverRevealAndDeadClick(t *testing.T) {
	eng := &scriptEngine{
		pages: map[string]*scriptPage{
			"http://ui.test/chats": {
				url:   "http://ui.test/chats",
				title: "Chats",
				nodes: []SnapNode{
					{Ref: "e1", Role: "link", Name: "Chat A", Tag: "a"},
					{Ref: "e2", Role: "link", Name: "Images", Tag: "a", Href: "http://ui.test/registry"},
					{Ref: "e3", Role: "button", Name: "Sign out", Tag: "button"},
				},
				revealed: map[string][]SnapNode{
					"e1": {{Ref: "e9", Role: "button", Name: "Delete chat", Tag: "button"}},
				},
				dead: map[string]bool{"e9": true},
				next: map[string]string{"e2": "http://ui.test/registry"},
			},
			"http://ui.test/registry": {
				url:   "http://ui.test/registry",
				title: "Images",
				nodes: []SnapNode{
					{Ref: "e1", Role: "heading", Name: "Images", Tag: "h1"},
				},
			},
		},
	}

	rep, err := Explore(t.Context(), eng, ExploreOpts{
		StartURL:  "http://ui.test/chats",
		MaxClicks: 8,
		MaxHovers: 8,
		MaxPages:  4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Pages) < 2 {
		t.Fatalf("pages=%d want at least chats+images: %+v", len(rep.Pages), rep.Pages)
	}

	var dead, reveal, skipped bool
	for _, f := range rep.Findings {
		switch f.Kind {
		case "dead_click":
			if strings.Contains(f.Target, "Delete chat") {
				dead = true
			}
		case "hover_reveal":
			if strings.Contains(f.Detail, "Delete chat") {
				reveal = true
			}
		case "skipped":
			if strings.Contains(f.Target, "Sign out") {
				skipped = true
			}
		}
	}
	if !reveal {
		t.Fatalf("expected hover_reveal of Delete chat: %+v", rep.Findings)
	}
	if !dead {
		t.Fatalf("expected dead_click on Delete chat: %+v", rep.Findings)
	}
	if !skipped {
		t.Fatalf("expected Sign out skipped: %+v", rep.Findings)
	}
	for _, c := range eng.clicks {
		if c == "e3" {
			t.Fatal("clicked Sign out")
		}
	}
}

func TestValidateStartURL(t *testing.T) {
	if err := ValidateStartURL("http://127.0.0.1:19000/chats"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStartURL("javascript:alert(1)"); err == nil {
		t.Fatal("expected reject")
	}
	if err := ValidateStartURL("/relative"); err == nil {
		t.Fatal("expected reject")
	}
}
