package promptsuggest

import (
	"strings"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

const (
	SourceConfig = "config"
	SourceLLM    = "llm"
	SourceNone   = "none"

	maxPrompts = 5
)

// FromConfig returns kind then global configured prompts.
func FromConfig(p settings.SuggestedPrompts, kind string) []string {
	return settings.ResolveSuggestedPrompts(p, kind)
}

func clampPrompts(list []string) []string {
	out := make([]string, 0, len(list))
	seen := map[string]struct{}{}
	for _, e := range list {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
		if len(out) >= maxPrompts {
			break
		}
	}
	return out
}
