package llmgw

import (
	"encoding/json"
	"testing"
)

func mustMatcher(t *testing.T, exact map[string]string, patterns []ModelPattern) *ModelMatcher {
	t.Helper()
	m, err := NewModelMatcher(exact, patterns)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestModelMatcher(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		m := mustMatcher(t, map[string]string{"gpt-alias": "gpt-real"}, nil)
		got, ok := m.Map("gpt-alias")
		if !ok || got != "gpt-real" {
			t.Fatalf("Map() = %q, %v; want gpt-real, true", got, ok)
		}
	})

	t.Run("wildcard match", func(t *testing.T) {
		m := mustMatcher(t, nil, []ModelPattern{
			{Pattern: "GPT-*", Target: "gpt-upstream"},
		})
		got, ok := m.Map("GPT-5.5-joybuilder")
		if !ok || got != "gpt-upstream" {
			t.Fatalf("Map() = %q, %v", got, ok)
		}
	})

	t.Run("regex match", func(t *testing.T) {
		m := mustMatcher(t, nil, []ModelPattern{
			{Pattern: `^claude-(.*)-alias$`, Regex: true, Target: "claude-sonnet-4"},
		})
		got, ok := m.Map("claude-dev-alias")
		if !ok || got != "claude-sonnet-4" {
			t.Fatalf("Map() = %q, %v", got, ok)
		}
	})

	t.Run("exact beats pattern", func(t *testing.T) {
		m := mustMatcher(t, map[string]string{"GPT-*": "exact-win"}, []ModelPattern{
			{Pattern: "GPT-*", Target: "pattern-lose"},
		})
		got, ok := m.Map("GPT-*")
		if !ok || got != "exact-win" {
			t.Fatalf("Map() = %q, %v", got, ok)
		}
	})

	t.Run("first pattern wins", func(t *testing.T) {
		m := mustMatcher(t, nil, []ModelPattern{
			{Pattern: "gpt-*", Target: "first"},
			{Pattern: "gpt-prod-*", Target: "second"},
		})
		got, ok := m.Map("gpt-prod-1")
		if !ok || got != "first" {
			t.Fatalf("Map() = %q, %v", got, ok)
		}
	})

	t.Run("no match", func(t *testing.T) {
		m := mustMatcher(t, map[string]string{"a": "b"}, nil)
		got, ok := m.Map("other")
		if ok || got != "other" {
			t.Fatalf("Map() = %q, %v", got, ok)
		}
	})

	t.Run("invalid glob", func(t *testing.T) {
		_, err := NewModelMatcher(nil, []ModelPattern{
			{Pattern: "[", Target: "x"},
		})
		if err == nil {
			t.Fatal("expected error for invalid glob")
		}
	})

	t.Run("invalid regex", func(t *testing.T) {
		_, err := NewModelMatcher(nil, []ModelPattern{
			{Pattern: "(", Regex: true, Target: "x"},
		})
		if err == nil {
			t.Fatal("expected error for invalid regex")
		}
	})
}

func TestMapModel(t *testing.T) {
	m := mustMatcher(t, map[string]string{"gpt-alias": "gpt-real"}, nil)

	t.Run("maps known model", func(t *testing.T) {
		in := []byte(`{"model":"gpt-alias","messages":[{"role":"user","content":"hi"}]}`)
		out, err := mapModel(in, m, "")
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(out, &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Model != "gpt-real" {
			t.Fatalf("model = %q, want gpt-real", parsed.Model)
		}
	})

	t.Run("passes through unknown model without default", func(t *testing.T) {
		in := []byte(`{"model":"other","messages":[]}`)
		out, err := mapModel(in, m, "")
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(in) {
			t.Fatalf("body changed: %s", out)
		}
	})

	t.Run("falls back to default for unknown model", func(t *testing.T) {
		in := []byte(`{"model":"other","messages":[]}`)
		out, err := mapModel(in, m, "gpt-default")
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(out, &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Model != "gpt-default" {
			t.Fatalf("model = %q, want gpt-default", parsed.Model)
		}
	})

	t.Run("keeps known upstream target", func(t *testing.T) {
		in := []byte(`{"model":"gpt-real","messages":[]}`)
		out, err := mapModel(in, m, "gpt-default")
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(in) {
			t.Fatalf("body changed: %s", out)
		}
	})

	t.Run("default with empty map", func(t *testing.T) {
		in := []byte(`{"model":"whatever"}`)
		out, err := mapModel(in, &ModelMatcher{}, "fallback")
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(out, &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Model != "fallback" {
			t.Fatalf("model = %q", parsed.Model)
		}
	})

	t.Run("passes through non json", func(t *testing.T) {
		in := []byte(`not json`)
		out, err := mapModel(in, m, "x")
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(in) {
			t.Fatalf("body changed: %s", out)
		}
	})

	t.Run("disabled matcher", func(t *testing.T) {
		in := []byte(`{"model":"gpt-alias"}`)
		out, err := mapModel(in, &ModelMatcher{}, "")
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(in) {
			t.Fatalf("body changed: %s", out)
		}
	})
}
