package llmgw

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
)

// ModelMatcher maps client model names to upstream model names.
type ModelMatcher struct {
	exact   map[string]string
	globs   []globRule
	regexes []regexRule
	targets map[string]struct{}
}

type globRule struct {
	pattern string
	target  string
}

type regexRule struct {
	re     *regexp.Regexp
	target string
}

// NewModelMatcher builds a matcher from exact map + ordered patterns.
func NewModelMatcher(exact map[string]string, patterns []ModelPattern) (*ModelMatcher, error) {
	m := &ModelMatcher{
		exact:   exact,
		targets: map[string]struct{}{},
	}
	if m.exact == nil {
		m.exact = map[string]string{}
	}
	for _, target := range m.exact {
		if target != "" {
			m.targets[target] = struct{}{}
		}
	}

	for i, p := range patterns {
		if p.Pattern == "" {
			return nil, fmt.Errorf("model_patterns[%d].pattern is empty", i)
		}
		if p.Target == "" {
			return nil, fmt.Errorf("model_patterns[%d].target is empty", i)
		}
		m.targets[p.Target] = struct{}{}
		if p.Regex {
			re, err := regexp.Compile(p.Pattern)
			if err != nil {
				return nil, fmt.Errorf("model_patterns[%d].pattern: %w", i, err)
			}
			m.regexes = append(m.regexes, regexRule{re: re, target: p.Target})
			continue
		}
		if _, err := filepath.Match(p.Pattern, "x"); err != nil {
			return nil, fmt.Errorf("model_patterns[%d].pattern: %w", i, err)
		}
		m.globs = append(m.globs, globRule{pattern: p.Pattern, target: p.Target})
	}

	return m, nil
}

// Enabled reports whether any mapping rules exist.
func (m *ModelMatcher) Enabled() bool {
	if m == nil {
		return false
	}
	return len(m.exact) > 0 || len(m.globs) > 0 || len(m.regexes) > 0
}

// IsKnownUpstream reports whether model is already an upstream target name.
func (m *ModelMatcher) IsKnownUpstream(model string) bool {
	if m == nil || model == "" {
		return false
	}
	_, ok := m.targets[model]
	return ok
}

// Map returns the upstream model name and whether a rule matched.
func (m *ModelMatcher) Map(model string) (string, bool) {
	if target, ok := m.exact[model]; ok {
		return target, true
	}
	for _, g := range m.globs {
		if ok, _ := filepath.Match(g.pattern, model); ok {
			return g.target, true
		}
	}
	for _, r := range m.regexes {
		if r.re.MatchString(model) {
			return r.target, true
		}
	}
	return model, false
}

// Resolve maps a client model, falling back to defaultModel when unknown.
// Known upstream target names pass through unchanged.
func (m *ModelMatcher) Resolve(model, defaultModel string) string {
	if mapped, ok := m.Map(model); ok {
		return mapped
	}
	if defaultModel == "" {
		return model
	}
	if m.IsKnownUpstream(model) {
		return model
	}
	return defaultModel
}

func mapModel(body []byte, matcher *ModelMatcher, defaultModel string) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	if !matcher.Enabled() && defaultModel == "" {
		return body, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, nil
	}

	raw, ok := obj["model"]
	if !ok {
		return body, nil
	}

	var model string
	if err := json.Unmarshal(raw, &model); err != nil {
		return body, nil
	}

	resolved := matcher.Resolve(model, defaultModel)
	if resolved == model {
		return body, nil
	}

	encoded, err := json.Marshal(resolved)
	if err != nil {
		return body, err
	}
	obj["model"] = encoded

	return json.Marshal(obj)
}
