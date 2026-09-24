package automode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Decision is the classifier verdict for one pending tool call.
type Decision string

const (
	Allow    Decision = "allow"
	SoftDeny Decision = "soft_deny"
	HardDeny Decision = "hard_deny"
)

// Request describes the pending tool call plus the context the classifier may
// use. Args is a rendered form of the tool input (usually JSON).
type Request struct {
	Name       string
	Title      string
	Kind       string
	Args       string
	Options    []string
	UserDigest string
}

// Verdict is the classifier's ruling. Rule is a short label, Reason is one
// user-facing sentence.
type Verdict struct {
	Decision Decision
	Rule     string
	Reason   string
}

// Allowed reports whether the call may run.
func (v Verdict) Allowed() bool { return v.Decision == Allow }

// Evaluator judges pending tool calls. Implementations must fail closed.
type Evaluator interface {
	Evaluate(ctx context.Context, req Request) (Verdict, error)
}

// LLMEvaluator classifies calls through the llmgw OpenAI-compatible API.
type LLMEvaluator struct {
	// Endpoint returns the relay base URL and model for the classifier; it is
	// called per evaluation so provider changes apply without a restart.
	Endpoint func() (baseURL, model string)
	APIKey   string
	Rules    func() Rules
	Client   *http.Client
}

// Evaluate asks the classifier model for a verdict. Infrastructure failures
// return a soft_deny verdict together with the error so callers can log the
// cause; a blocked call is always the safe outcome.
func (e *LLMEvaluator) Evaluate(ctx context.Context, req Request) (Verdict, error) {
	baseURL, model := "", ""
	if e != nil && e.Endpoint != nil {
		baseURL, model = e.Endpoint()
	}
	if strings.TrimSpace(baseURL) == "" {
		err := errors.New("not configured")
		return failClosed(err), err
	}
	if strings.TrimSpace(model) == "" {
		model = "default"
	}
	rules := Rules{}
	if e.Rules != nil {
		rules = Expand(e.Rules())
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt(rules, req)},
		},
		"temperature": 0,
		"max_tokens":  600,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return failClosed(err), err
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return failClosed(err), err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	res, err := client.Do(httpReq)
	if err != nil {
		return failClosed(err), err
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		err := fmt.Errorf("classifier http %d: %s", res.StatusCode, truncateRunes(string(respBody), 200))
		return failClosed(err), err
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return failClosed(err), err
	}
	if len(envelope.Choices) == 0 {
		err := errors.New("classifier returned no choices")
		return failClosed(err), err
	}
	verdict, err := parseVerdict(envelope.Choices[0].Message.Content)
	if err != nil {
		return failClosed(err), err
	}
	return verdict, nil
}

func parseVerdict(content string) (Verdict, error) {
	content = stripFences(content)
	var raw struct {
		Decision string `json:"decision"`
		Rule     string `json:"rule"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return Verdict{}, fmt.Errorf("parse verdict json: %w", err)
	}
	var decision Decision
	switch strings.ToLower(strings.TrimSpace(raw.Decision)) {
	case "allow", "allowed":
		decision = Allow
	case "soft_deny", "soft", "deny", "block":
		decision = SoftDeny
	case "hard_deny", "hard":
		decision = HardDeny
	default:
		return Verdict{}, fmt.Errorf("unknown decision %q", raw.Decision)
	}
	rule := strings.TrimSpace(raw.Rule)
	if rule == "" {
		rule = "auto mode"
	}
	reason := strings.TrimSpace(raw.Reason)
	if reason == "" && decision != Allow {
		reason = "the action may be destructive or send data outside the workspace"
	}
	return Verdict{Decision: decision, Rule: rule, Reason: reason}, nil
}

func failClosed(err error) Verdict {
	return Verdict{
		Decision: SoftDeny,
		Rule:     "classifier_unavailable",
		Reason:   "auto mode classifier unavailable: " + err.Error(),
	}
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
