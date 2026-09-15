package sysagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

// scriptedClient answers permission requests from a script keyed by tool-call
// title, and records every prompt so tests can assert which calls needed one.
type scriptedClient struct {
	*captureClient
	mu        sync.Mutex
	answers   map[string]string // request ToolCall title → option id
	prompted  []string
	toolCalls map[string]string // tool call id → result text
}

func newScriptedClient(answers map[string]string) *scriptedClient {
	return &scriptedClient{
		captureClient: &captureClient{},
		answers:       answers,
		toolCalls:     map[string]string{},
	}
}

func (c *scriptedClient) RequestPermission(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	c.mu.Lock()
	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}
	c.prompted = append(c.prompted, title)
	id, ok := c.answers[title]
	c.mu.Unlock()
	if !ok {
		id = "allow"
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: acp.PermissionOptionId(id)},
		},
	}, nil
}

func (c *scriptedClient) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	if u := n.Update.ToolCallUpdate; u != nil && u.Status != nil && *u.Status == acp.ToolCallStatusCompleted {
		for _, blk := range u.Content {
			if blk.Content != nil && blk.Content.Content.Text != nil {
				c.mu.Lock()
				c.toolCalls[string(u.ToolCallId)] = blk.Content.Content.Text.Text
				c.mu.Unlock()
			}		}
	}
	return c.captureClient.SessionUpdate(ctx, n)
}

func (c *scriptedClient) promptCount(title string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, p := range c.prompted {
		if p == title {
			n++
		}
	}
	return n
}

// llmScript serves one scripted assistant round per request. Each script entry
// is the JSON tool_calls payload (or {"content": ...} for a plain reply).
func llmScript(script []string) *httptest.Server {
	var round int
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		round++
		n := round
		mu.Unlock()
		if n > len(script) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"finish_reason": "stop",
					"message":       map[string]any{"role": "assistant", "content": "done"},
				}},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, script[n-1])
	}))
}

func toolCallJSON(id, name, args string) string {
	return fmt.Sprintf(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`, id, name, args)
}

// driveAgent wires the agent over ACP pipes like production and runs one
// prompt. A prompt-level error fails the test; callers that expect one use
// driveAgentE.
func driveAgent(t *testing.T, agent *sysagent.Agent, client acp.Client, prompt string) acp.PromptResponse {
	t.Helper()
	resp, err := driveAgentE(t, agent, client, prompt)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func driveAgentE(t *testing.T, agent *sysagent.Agent, client acp.Client, prompt string) (acp.PromptResponse, error) {
	t.Helper()
	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()
	asc := acp.NewAgentSideConnection(agent, a2cW, c2aR)
	agent.SetAgentConnection(asc)
	csc := acp.NewClientSideConnection(client, c2aW, a2cR)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := csc.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber}); err != nil {
		t.Fatal(err)
	}
	sess, err := csc.NewSession(ctx, acp.NewSessionRequest{Cwd: ".", McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	return csc.Prompt(ctx, acp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock(prompt)},
	})
}

func mutatingReg() *tools.Registry {
	reg := tools.NewRegistry()
	tools.RegisterInteractive(reg)
	reg.Register(tools.Tool{
		Name:        "mutate",
		Description: "mutate workspace",
		Mutating:    true,
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(context.Context, tools.Actor, json.RawMessage) (string, error) {
			return "mutated", nil
		},
	})
	// A Skill-shaped tool: mutating per its flag, but the dispatch gate decides
	// per action (invoke/list read-only, install/remove mutating).
	reg.Register(tools.Tool{
		Name:        "Skill",
		Description: "skills",
		Mutating:    true,
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(_ context.Context, _ tools.Actor, args json.RawMessage) (string, error) {
			var in struct {
				Action string `json:"action"`
			}
			_ = json.Unmarshal(args, &in)
			if in.Action == "" {
				in.Action = "invoke"
			}
			return "skill-" + in.Action, nil
		},
	})
	return reg
}

func TestAgent_PlanModeGatesMutatingTools(t *testing.T) {
	llm := llmScript([]string{
		toolCallJSON("c1", "EnterPlanMode", `{}`),
		toolCallJSON("c2", "mutate", `{}`),
		toolCallJSON("c3", "ExitPlanMode", `{"plan":"step 1"}`),
		toolCallJSON("c4", "mutate", `{}`),
	})
	defer llm.Close()

	agent := sysagent.New(sysagent.Deps{
		LLM:   sysagent.LLMConfig{BaseURL: llm.URL, APIKey: "k", Model: "m", HTTPClient: llm.Client()},
		Tools: mutatingReg(),
		Actor: tools.Actor{Username: "u"},
	})
	client := newScriptedClient(map[string]string{
		"Plan approval · Approve this plan?\n\nstep 1": "opt-0",
	})
	driveAgent(t, agent, client, "plan then do it")

	statuses := client.toolStatuses()
	byID := map[string]string{}
	for _, u := range statuses {
		byID[u.id] = u.status
	}
	if byID["c1"] != "completed" || byID["c3"] != "completed" {
		t.Fatalf("plan mode calls should complete: %v", byID)
	}
	if byID["c2"] != "failed" {
		t.Fatalf("mutate in plan mode should fail: %v", byID)
	}
	if byID["c4"] != "completed" {
		t.Fatalf("mutate after approval should complete: %v", byID)
	}
	if got := client.toolCalls["c4"]; got != "mutated" {
		t.Fatalf("c4 result = %q", got)
	}
	if client.promptCount("mutate") != 1 {
		t.Fatalf("mutate should be prompted exactly once (after plan approval), got %d", client.promptCount("mutate"))
	}
}

func TestAgent_SkillActionGating(t *testing.T) {
	llm := llmScript([]string{
		toolCallJSON("c1", "EnterPlanMode", `{}`),
		toolCallJSON("c2", "Skill", `{"action":"invoke","skill":"commit"}`),
		toolCallJSON("c3", "Skill", `{"action":"install","content":"x"}`),
		toolCallJSON("c4", "ExitPlanMode", `{"plan":"p"}`),
		toolCallJSON("c5", "Skill", `{"action":"install","content":"x"}`),
	})
	defer llm.Close()

	agent := sysagent.New(sysagent.Deps{
		LLM:   sysagent.LLMConfig{BaseURL: llm.URL, APIKey: "k", Model: "m", HTTPClient: llm.Client()},
		Tools: mutatingReg(),
		Actor: tools.Actor{Username: "u"},
	})
	client := newScriptedClient(map[string]string{
		"Plan approval · Approve this plan?\n\np": "opt-0",
		"Skill":   "allow",
	})
	driveAgent(t, agent, client, "skills")

	byID := map[string]string{}
	for _, u := range client.toolStatuses() {
		byID[u.id] = u.status
	}
	if byID["c2"] != "completed" {
		t.Fatalf("skill invoke is read-only and must run in plan mode: %v", byID)
	}
	if byID["c3"] != "failed" {
		t.Fatalf("skill install must be blocked in plan mode: %v", byID)
	}
	if byID["c5"] != "completed" {
		t.Fatalf("skill install after approval should complete: %v", byID)
	}
	if got := client.toolCalls["c5"]; got != "skill-install" {
		t.Fatalf("c5 result = %q", got)
	}
	// Only the install (c5) prompts; invoke (c2) and the blocked install (c3) do not.
	if client.promptCount("Skill") != 1 {
		t.Fatalf("Skill should be prompted once, got %d", client.promptCount("Skill"))
	}
}

func TestAgent_AskUserQuestion(t *testing.T) {
	llm := llmScript([]string{
		toolCallJSON("c1", "AskUserQuestion", `{"question":"Which lib?","header":"Library","options":[{"label":"A"},{"label":"B"}]}`),
		toolCallJSON("c2", "AskUserQuestion", `{"question":"Bad?","header":"X","options":[{"label":"only one"}]}`),
	})
	defer llm.Close()

	reg := tools.NewRegistry()
	tools.RegisterInteractive(reg)
	agent := sysagent.New(sysagent.Deps{
		LLM:   sysagent.LLMConfig{BaseURL: llm.URL, APIKey: "k", Model: "m", HTTPClient: llm.Client()},
		Tools: reg,
		Actor: tools.Actor{Username: "u"},
	})
	client := newScriptedClient(map[string]string{
		"Library · Which lib?": "opt-1",
	})
	driveAgent(t, agent, client, "ask me")

	byID := map[string]string{}
	for _, u := range client.toolStatuses() {
		byID[u.id] = u.status
	}
	if byID["c1"] != "completed" {
		t.Fatalf("valid question should complete: %v", byID)
	}
	if got := client.toolCalls["c1"]; !strings.Contains(got, `"answer":"B"`) {
		t.Fatalf("expected opt-1 to map back to label B, got %q", got)
	}
	if byID["c2"] != "failed" {
		t.Fatalf("one-option question should fail validation: %v", byID)
	}
}

func TestAgent_TrivialACPMethods(t *testing.T) {
	agent := sysagent.New(sysagent.Deps{Actor: tools.Actor{Username: "u"}})
	ctx := context.Background()
	if _, err := agent.Authenticate(ctx, acp.AuthenticateRequest{}); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	// SetSessionMode is a successful no-op; the rest report method-not-found.
	if _, err := agent.SetSessionMode(ctx, acp.SetSessionModeRequest{}); err != nil {
		t.Errorf("SetSessionMode should be a no-op success: %v", err)
	}
	for name, call := range map[string]func() error{
		"Logout": func() error {
			_, err := agent.Logout(ctx, acp.LogoutRequest{})
			return err
		},
		"CloseSession": func() error {
			_, err := agent.CloseSession(ctx, acp.CloseSessionRequest{})
			return err
		},
		"ListSessions": func() error {
			_, err := agent.ListSessions(ctx, acp.ListSessionsRequest{})
			return err
		},
		"ResumeSession": func() error {
			_, err := agent.ResumeSession(ctx, acp.ResumeSessionRequest{})
			return err
		},
		"SetSessionConfigOption": func() error {
			_, err := agent.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s: expected method-not-found or no-op, got success", name)
		}
	}
}
