package promptsuggest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Generator asks llmgw for starter prompts when config is empty.
type Generator struct {
	Endpoint func() (baseURL, model string)
	APIKey   string
	Client   *http.Client
}

// Context is compressed assistant / env / session info for the model.
type Context struct {
	Name         string
	Bio          string
	Kind         string
	Capabilities string
	Network      string
	Grants       []string
	Recent       []string
}

const (
	generateTimeout = 8 * time.Second
	maxPromptRunes  = 200
)

func (g *Generator) Generate(ctx context.Context, in Context) ([]string, error) {
	if g == nil || g.Endpoint == nil {
		return nil, fmt.Errorf("generator not configured")
	}
	baseURL, model := g.Endpoint()
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("llm endpoint not configured")
	}
	if strings.TrimSpace(model) == "" {
		model = "default"
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: generateTimeout}
	}
	ctx, cancel := context.WithTimeout(ctx, generateTimeout)
	defer cancel()

	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt(in)},
		},
		"temperature": 0.4,
		"max_tokens":  500,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.APIKey)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("suggest http %d: %s", res.StatusCode, truncateRunes(string(respBody), 200))
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Choices) == 0 {
		return nil, fmt.Errorf("suggest returned no choices")
	}
	return parsePrompts(envelope.Choices[0].Message.Content)
}

const systemPrompt = `You suggest short starter chat prompts a user can click to begin talking to an assistant.
Return ONLY a JSON array of 3 strings (no object wrapper). Each string is one spoken prompt in the user's language (prefer Chinese if the assistant name/bio is Chinese, otherwise English).
Rules: under 60 characters when possible; concrete and actionable; no numbering; no markdown.`

func userPrompt(in Context) string {
	var b strings.Builder
	b.WriteString("Assistant:\n")
	fmt.Fprintf(&b, "- name: %s\n", truncateRunes(in.Name, 80))
	fmt.Fprintf(&b, "- kind: %s\n", truncateRunes(in.Kind, 40))
	if bio := strings.TrimSpace(in.Bio); bio != "" {
		fmt.Fprintf(&b, "- bio: %s\n", truncateRunes(bio, 240))
	}
	if caps := strings.TrimSpace(in.Capabilities); caps != "" {
		fmt.Fprintf(&b, "- capabilities: %s\n", truncateRunes(caps, 120))
	}
	if net := strings.TrimSpace(in.Network); net != "" {
		fmt.Fprintf(&b, "- network: %s\n", truncateRunes(net, 40))
	}
	if len(in.Grants) > 0 {
		b.WriteString("- directory grants:\n")
		for i, g := range in.Grants {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&b, "  - %s\n", truncateRunes(g, 120))
		}
	}
	if len(in.Recent) > 0 {
		b.WriteString("- recent conversation snippets:\n")
		for i, line := range in.Recent {
			if i >= 6 {
				break
			}
			fmt.Fprintf(&b, "  - %s\n", truncateRunes(line, 160))
		}
	}
	b.WriteString("\nSuggest 3 starter prompts for an empty or early session.")
	return b.String()
}

func parsePrompts(content string) ([]string, error) {
	content = stripFences(content)
	var list []string
	if err := json.Unmarshal([]byte(content), &list); err != nil {
		// Tolerate {"prompts":[...]}.
		var wrap struct {
			Prompts []string `json:"prompts"`
		}
		if err2 := json.Unmarshal([]byte(content), &wrap); err2 != nil {
			return nil, fmt.Errorf("parse prompts json: %w", err)
		}
		list = wrap.Prompts
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if utf8.RuneCountInString(e) > maxPromptRunes {
			e = truncateRunes(e, maxPromptRunes)
		}
		out = append(out, e)
	}
	out = clampPrompts(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("empty prompt list")
	}
	return out, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	return s
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
