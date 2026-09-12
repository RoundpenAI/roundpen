package hostsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Reconcile drops unknown actions, skips already-satisfied facts, and fills
// title/command/sensitive/privilege from the whitelist catalog.
func Reconcile(raw Plan, f HostFacts, priv Privilege, w WizardContext) Plan {
	needBrowser := w.Preset == "code_browser"
	var out []PlannedAction
	seen := map[string]bool{}
	for _, a := range raw.Actions {
		def, ok := LookupAction(a.ID)
		if !ok || seen[a.ID] {
			continue
		}
		switch a.ID {
		case ActionInstallDocker:
			if f.DockerReady {
				continue
			}
		case ActionInstallQEMU:
			if f.BinariesOK {
				continue
			}
		case ActionBuildBrowserImage:
			if !needBrowser || f.BrowserImageOK {
				continue
			}
		}
		seen[a.ID] = true
		pa := PlannedAction{
			ID:        a.ID,
			Title:     def.Title,
			Reason:    strings.TrimSpace(a.Reason),
			Sensitive: def.Sensitive,
			Command:   commandFor(a.ID),
		}
		if pa.Reason == "" {
			pa.Reason = "根据环境检测需要此步骤"
		}
		if def.Sensitive {
			pa.Privilege = priv
		}
		out = append(out, pa)
	}
	summary := strings.TrimSpace(raw.Summary)
	if summary == "" {
		summary = PlanFromFacts(w, f, priv).Summary
	}
	if len(out) == 0 {
		summary = "工位已就绪"
	}
	return Plan{Summary: summary, Actions: out}
}

// LLMPlanner calls the local LLMGW OpenAI-compatible chat API.
type LLMPlanner struct {
	BaseURL string // e.g. http://127.0.0.1:19001
	APIKey  string
	Model   string
	Client  *http.Client
}

func (p *LLMPlanner) Plan(ctx context.Context, w WizardContext, f HostFacts) (Plan, error) {
	if p == nil || strings.TrimSpace(p.BaseURL) == "" {
		return Plan{}, fmt.Errorf("llm planner not configured")
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	sys := `你是 Roundpen 工位准备规划器。只输出 JSON，不要 markdown。
格式: {"summary":"中文短句","actions":[{"id":"...","title":"...","reason":"..."}]}
id 只能是: install_docker, install_qemu, build_browser_image。
不要输出 shell 命令。已就绪的项不要列入。`
	user := fmt.Sprintf(
		"wizard=%s preset=%s bio=%q\nfacts dockerReady=%v binariesOK=%v browserImageOK=%v",
		w.Name, w.Preset, w.Bio, f.DockerReady, f.BinariesOK, f.BrowserImageOK,
	)
	body := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": user},
		},
		"temperature": 0,
	}
	raw, _ := json.Marshal(body)
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Plan{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	res, err := client.Do(req)
	if err != nil {
		return Plan{}, err
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return Plan{}, fmt.Errorf("llm http %d: %s", res.StatusCode, truncate(string(respBody), 200))
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return Plan{}, err
	}
	if len(envelope.Choices) == 0 {
		return Plan{}, fmt.Errorf("llm empty choices")
	}
	content := stripFences(envelope.Choices[0].Message.Content)
	var plan Plan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return Plan{}, fmt.Errorf("parse plan json: %w", err)
	}
	return plan, nil
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
