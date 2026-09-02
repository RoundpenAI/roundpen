package browser

import (
	"context"
	"encoding/json"
)

// Engine drives one browser session (Chrome CDP or a test fake).
type Engine interface {
	Navigate(ctx context.Context, url string) error
	URL() string
	Title(ctx context.Context) (string, error)
	Snapshot(ctx context.Context) (Snapshot, error)
	Click(ctx context.Context, ref string) error
	Type(ctx context.Context, ref, text string, submit bool) error
	Press(ctx context.Context, key string) error
	Screenshot(ctx context.Context) ([]byte, error)
	SetViewport(ctx context.Context, width, height int) error
	Evaluate(ctx context.Context, expression string) (json.RawMessage, error)
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
	Ref     string `json:"ref"`
	Role    string `json:"role"`
	Name    string `json:"name"`
	Tag     string `json:"tag,omitempty"`
	Value   string `json:"value,omitempty"`
	Href    string `json:"href,omitempty"`
	Checked *bool  `json:"checked,omitempty"`
}
