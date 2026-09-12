package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// BrowserSlot starts the user's Browser environment (Chrome / CDP).
type BrowserSlot interface {
	EnsureBrowser(ctx context.Context, userID string) (*sandbox.Sandbox, error)
}

// BrowserBinder attaches a browser Engine for System Agent sessions.
type BrowserBinder struct {
	Hub       *browser.Hub
	Slots     BrowserSlot
	SessionID string
}

func (b *BrowserBinder) resolveID(ctx context.Context, userID string) (string, error) {
	if b != nil && b.Slots != nil && strings.TrimSpace(userID) != "" {
		sb, err := b.Slots.EnsureBrowser(ctx, userID)
		if err != nil {
			return "", fmt.Errorf("ensure browser environment: %w", err)
		}
		if sb == nil || sb.ID == "" {
			return "", fmt.Errorf("browser is not available")
		}
		return sb.ID, nil
	}
	if b == nil {
		return browser.AgentBrowserID(""), nil
	}
	return browser.AgentBrowserID(b.SessionID), nil
}

func (b *BrowserBinder) ensure(ctx context.Context, actor Actor) (*browser.Session, error) {
	if b == nil || b.Hub == nil {
		return nil, WrapBrowserEnsure(fmt.Errorf("browser hub not configured"))
	}
	id, err := b.resolveID(ctx, actor.Username)
	if err != nil {
		return nil, WrapBrowserEnsure(err)
	}
	sess, err := b.Hub.Ensure(ctx, id)
	if err != nil {
		return nil, WrapBrowserEnsure(err)
	}
	return sess, nil
}

func (b *BrowserBinder) guardAgentControl(ctx context.Context, actor Actor) error {
	if b == nil || b.Hub == nil {
		return fmt.Errorf("browser hub not configured")
	}
	id, err := b.resolveID(ctx, actor.Username)
	if err != nil {
		return err
	}
	if b.Hub.Takeover(id) {
		return fmt.Errorf("browser under human takeover")
	}
	return nil
}

// RegisterBrowser adds browser_* tools (aligned with Browser MCP).
// Mutating tools: navigate, click, type, evaluate.
func RegisterBrowser(r *Registry, binder *BrowserBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name:        "browser_navigate",
		Description: "Navigate the System Agent browser to a URL.",
		Mutating:    true,
		Parameters: objectSchema(map[string]any{
			"url": map[string]any{"type": "string"},
		}, "url"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			if err := binder.guardAgentControl(ctx, actor); err != nil {
				return "", err
			}
			var in struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.URL) == "" {
				return "", fmt.Errorf("url is required")
			}
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			if err := sess.Engine.Navigate(ctx, in.URL); err != nil {
				return "", err
			}
			return fmt.Sprintf(`{"ok":true,"url":%q}`, sess.Engine.URL()), nil
		},
	})
	r.Register(Tool{
		Name:        "browser_snapshot",
		Description: "Accessibility-ish snapshot of the current page (refs for click/type).",
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			snap, err := sess.Engine.Snapshot(ctx)
			if err != nil {
				return "", err
			}
			raw, err := json.Marshal(snap)
			return string(raw), err
		},
	})
	r.Register(Tool{
		Name:        "browser_hover",
		Description: "Hover an element by snapshot ref so CSS :hover and mouseover handlers can reveal hidden actions.",
		Parameters: objectSchema(map[string]any{
			"ref": map[string]any{"type": "string"},
		}, "ref"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			if err := binder.guardAgentControl(ctx, actor); err != nil {
				return "", err
			}
			var in struct {
				Ref string `json:"ref"`
			}
			if err := json.Unmarshal(args, &in); err != nil || in.Ref == "" {
				return "", fmt.Errorf("ref is required")
			}
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			if err := sess.Engine.Hover(ctx, in.Ref); err != nil {
				return "", err
			}
			return `{"ok":true}`, nil
		},
	})
	r.Register(Tool{
		Name:        "browser_explore",
		Description: "Structural walk of a URL: hover to reveal hidden controls, click interactive elements, report dead clicks and page errors. Prefer this before ad-hoc clicking when exploring or verifying a product.",
		Mutating:    true,
		Parameters: objectSchema(map[string]any{
			"url":       map[string]any{"type": "string"},
			"maxClicks": map[string]any{"type": "integer"},
			"maxHovers": map[string]any{"type": "integer"},
			"maxPages":  map[string]any{"type": "integer"},
		}, "url"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			if err := binder.guardAgentControl(ctx, actor); err != nil {
				return "", err
			}
			var in struct {
				URL       string `json:"url"`
				MaxClicks int    `json:"maxClicks"`
				MaxHovers int    `json:"maxHovers"`
				MaxPages  int    `json:"maxPages"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.URL) == "" {
				return "", fmt.Errorf("url is required")
			}
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			rep, err := browser.Explore(ctx, sess.Engine, browser.ExploreOpts{
				StartURL:  in.URL,
				MaxClicks: in.MaxClicks,
				MaxHovers: in.MaxHovers,
				MaxPages:  in.MaxPages,
			})
			if err != nil {
				return "", err
			}
			raw, err := json.Marshal(rep)
			return string(raw), err
		},
	})
	r.Register(Tool{
		Name:        "browser_click",
		Description: "Click an element by snapshot ref.",
		Mutating:    true,
		Parameters: objectSchema(map[string]any{
			"ref": map[string]any{"type": "string"},
		}, "ref"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			if err := binder.guardAgentControl(ctx, actor); err != nil {
				return "", err
			}
			var in struct {
				Ref string `json:"ref"`
			}
			if err := json.Unmarshal(args, &in); err != nil || in.Ref == "" {
				return "", fmt.Errorf("ref is required")
			}
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			if err := sess.Engine.Click(ctx, in.Ref); err != nil {
				return "", err
			}
			return `{"ok":true}`, nil
		},
	})
	r.Register(Tool{
		Name:        "browser_type",
		Description: "Type text into an element by ref.",
		Mutating:    true,
		Parameters: objectSchema(map[string]any{
			"ref":    map[string]any{"type": "string"},
			"text":   map[string]any{"type": "string"},
			"submit": map[string]any{"type": "boolean"},
		}, "ref", "text"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			if err := binder.guardAgentControl(ctx, actor); err != nil {
				return "", err
			}
			var in struct {
				Ref    string `json:"ref"`
				Text   string `json:"text"`
				Submit bool   `json:"submit"`
			}
			if err := json.Unmarshal(args, &in); err != nil || in.Ref == "" {
				return "", fmt.Errorf("ref and text are required")
			}
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			if err := sess.Engine.Type(ctx, in.Ref, in.Text, in.Submit); err != nil {
				return "", err
			}
			return `{"ok":true}`, nil
		},
	})
	r.Register(Tool{
		Name:        "browser_press",
		Description: "Press a key (e.g. Enter, Tab).",
		Parameters: objectSchema(map[string]any{
			"key": map[string]any{"type": "string"},
		}, "key"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			if err := binder.guardAgentControl(ctx, actor); err != nil {
				return "", err
			}
			var in struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(args, &in); err != nil || in.Key == "" {
				return "", fmt.Errorf("key is required")
			}
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			if err := sess.Engine.Press(ctx, in.Key); err != nil {
				return "", err
			}
			return `{"ok":true}`, nil
		},
	})
	r.Register(Tool{
		Name:        "browser_screenshot",
		Description: "Take a PNG screenshot; returns base64 data.",
		Parameters:  objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			png, err := sess.Engine.Screenshot(ctx)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(`{"mime":"image/png","base64":%q}`, base64.StdEncoding.EncodeToString(png)), nil
		},
	})
	r.Register(Tool{
		Name:        "browser_set_viewport",
		Description: "Set browser viewport size.",
		Parameters: objectSchema(map[string]any{
			"width":  map[string]any{"type": "integer"},
			"height": map[string]any{"type": "integer"},
		}, "width", "height"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			if err := binder.guardAgentControl(ctx, actor); err != nil {
				return "", err
			}
			var in struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			}
			if err := json.Unmarshal(args, &in); err != nil || in.Width <= 0 || in.Height <= 0 {
				return "", fmt.Errorf("width and height are required")
			}
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			if err := sess.Engine.SetViewport(ctx, in.Width, in.Height); err != nil {
				return "", err
			}
			sess.Width, sess.Height = in.Width, in.Height
			return `{"ok":true}`, nil
		},
	})
	r.Register(Tool{
		Name:        "browser_evaluate",
		Description: "Evaluate a JavaScript expression in the page.",
		Mutating:    true,
		Parameters: objectSchema(map[string]any{
			"expression": map[string]any{"type": "string"},
		}, "expression"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			if err := binder.guardAgentControl(ctx, actor); err != nil {
				return "", err
			}
			var in struct {
				Expression string `json:"expression"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Expression) == "" {
				return "", fmt.Errorf("expression is required")
			}
			sess, err := binder.ensure(ctx, actor)
			if err != nil {
				return "", err
			}
			raw, err := sess.Engine.Evaluate(ctx, in.Expression)
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
	})
}
