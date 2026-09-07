// Package tools registers callable tools for the System Agent.
//
// Future *-use tools (e.g. computer_use_*) register here alongside Roundpen and browser tools.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Actor is the authenticated Roundpen user the agent acts as.
type Actor struct {
	Username string
	Role     string
	APIKey   string
}

// Tool is one LLM-callable function.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
	Mutating    bool
	Call        func(ctx context.Context, actor Actor, args json.RawMessage) (string, error)
}

// Registry holds tools by name.
type Registry struct {
	byName map[string]Tool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Tool)}
}

// Register adds or replaces a tool.
func (r *Registry) Register(t Tool) {
	if r.byName == nil {
		r.byName = make(map[string]Tool)
	}
	r.byName[t.Name] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// List returns tools sorted by name.
func (r *Registry) List() []Tool {
	out := make([]Tool, 0, len(r.byName))
	for _, t := range r.byName {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// OpenAITools formats tools for chat/completions "tools" array.
func (r *Registry) OpenAITools() []map[string]any {
	list := r.List()
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		})
	}
	return out
}

// Call runs a tool by name.
func (r *Registry) Call(ctx context.Context, actor Actor, name string, args json.RawMessage) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return t.Call(ctx, actor, args)
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
