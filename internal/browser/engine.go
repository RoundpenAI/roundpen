package browser

import (
	"context"
	"encoding/json"
	"strings"
)

// Engine drives one browser session (Chrome CDP or a test fake).
type Engine interface {
	Navigate(ctx context.Context, url string) error
	URL() string
	Title(ctx context.Context) (string, error)
	Snapshot(ctx context.Context) (Snapshot, error)
	Hover(ctx context.Context, ref string) error
	Click(ctx context.Context, ref string) error
	Type(ctx context.Context, ref, text string, submit bool) error
	Press(ctx context.Context, key string) error
	Screenshot(ctx context.Context) ([]byte, error)
	SetViewport(ctx context.Context, width, height int) error
	Evaluate(ctx context.Context, expression string) (json.RawMessage, error)
	// Pixel-level input for human takeover (CSS viewport coordinates).
	InputClick(ctx context.Context, x, y float64) error
	InputMove(ctx context.Context, x, y float64) error
	InputWheel(ctx context.Context, x, y, deltaX, deltaY float64) error
	InputType(ctx context.Context, text string) error
	InputKey(ctx context.Context, key string) error
	Close() error
}

// Snapshot is a compact accessibility-ish view of the current page.
type Snapshot struct {
	URL    string     `json:"url"`
	Title  string     `json:"title"`
	Text   string     `json:"text"`
	Nodes  []SnapNode `json:"nodes"`
	Width  int        `json:"width,omitempty"`
	Height int        `json:"height,omitempty"`
}

// SnapNode is one interactive or labelled element.
type SnapNode struct {
	Ref     string  `json:"ref"`
	Role    string  `json:"role"`
	Name    string  `json:"name"`
	Tag     string  `json:"tag,omitempty"`
	Value   string  `json:"value,omitempty"`
	Href    string  `json:"href,omitempty"`
	X       float64 `json:"x,omitempty"`
	Y       float64 `json:"y,omitempty"`
	W       float64 `json:"w,omitempty"`
	H       float64 `json:"h,omitempty"`
	Checked *bool   `json:"checked,omitempty"`
}

// AgentBrowserID maps a Roundpen agent session id to a Hub session key.
func AgentBrowserID(sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return "sysagent-default"
	}
	if strings.HasPrefix(id, "sysagent-") {
		return id
	}
	return "sysagent-" + id
}
