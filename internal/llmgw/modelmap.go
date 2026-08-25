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
	m := &ModelMatcher{exact: exact}
	if m.exact == nil {
		m.exact = map[string]string{}
	}

	for i, p := range patterns {
		if p.Pattern == "" {
			return nil, fmt.Errorf("model_patterns[%d].pattern is empty", i)
		}
		if p.Target == "" {
			return nil, fmt.Errorf("model_patterns[%d].target is empty", i)
		}
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

func mapModel(body []byte, matcher *ModelMatcher) ([]byte, error) {
	if !matcher.Enabled() || len(body) == 0 {
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

	mapped, ok := matcher.Map(model)
	if !ok {
		return body, nil
	}

	encoded, err := json.Marshal(mapped)
	if err != nil {
		return body, err
	}
	obj["model"] = encoded

	return json.Marshal(obj)
}
