package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/photos"
)

// RegisterOnThisDay adds the OnThisDay photo anniversary tool.
func RegisterOnThisDay(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "OnThisDay",
		Description: "Find photos under granted host directories that match today's month/day in prior years " +
			"(filename date or file mtime). Use ListDirectoryGrants first. Optional years_ago filters to one anniversary " +
			"(1 = last year). Then use ReadImage to show picks and HostCopy / HostApplyPlan to copy into a memories folder.",
		Parameters: objectSchema(map[string]any{
			"years_ago": map[string]any{"type": "integer", "description": "Only this anniversary (0 = all prior years)"},
			"max_hits":  map[string]any{"type": "integer", "description": "Max results (default 30, max 100)"},
			"root":      map[string]any{"type": "string", "description": "Optional single granted root; default all grants"},
		}),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				YearsAgo int    `json:"years_ago"`
				MaxHits  int    `json:"max_hits"`
				Root     string `json:"root"`
			}
			_ = json.Unmarshal(args, &in)
			return binder.onThisDay(ctx, actor, in.YearsAgo, in.MaxHits, in.Root)
		},
	})
}

func (b *AgentBinder) onThisDay(ctx context.Context, actor Actor, yearsAgo, maxHits int, root string) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	grants, err := host.ListGrants(ctx, actor)
	if err != nil {
		return "", err
	}
	var roots []string
	want := strings.TrimSpace(root)
	for _, g := range grants {
		p := strings.TrimSpace(g.Path)
		if p == "" {
			continue
		}
		if want != "" && p != want && !strings.HasPrefix(want, p+"/") && !strings.HasPrefix(p, want+"/") {
			if want != p {
				continue
			}
		}
		if want != "" {
			roots = []string{want}
			break
		}
		roots = append(roots, p)
	}
	if want != "" && len(roots) == 0 {
		roots = []string{want}
	}
	if len(roots) == 0 {
		return `{"hits":[],"note":"no directory grants — ask the user to authorize a photos folder"}`, nil
	}
	if maxHits <= 0 {
		maxHits = 30
	}
	if maxHits > 100 {
		maxHits = 100
	}
	hits, err := photos.ScanOnThisDay(ctx, photos.ScanOptions{
		Roots: roots, Now: time.Now(), MaxHits: maxHits, YearsAgo: yearsAgo,
	})
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(map[string]any{
		"hits":  hits,
		"count": len(hits),
		"when":  fmt.Sprintf("%s (years_ago filter=%d)", time.Now().Format("01-02"), yearsAgo),
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
