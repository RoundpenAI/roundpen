package sysagent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/automode"
)

type stubEvaluator struct {
	verdict automode.Verdict
	err     error
	mu      sync.Mutex
	reqs    []automode.Request
}

func (s *stubEvaluator) Evaluate(_ context.Context, req automode.Request) (automode.Verdict, error) {
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	return s.verdict, s.err
}

func (s *stubEvaluator) lastRequest() automode.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return automode.Request{}
	}
	return s.reqs[len(s.reqs)-1]
}

// autoModeLLM answers the first round with one mutating tool call and the
// second with a stop. Request bodies are captured for tool-result assertions.
func autoModeLLM(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	var round atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		if round.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"finish_reason": "tool_calls",
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      "mutate_thing",
								"arguments": `{"target":"/etc/passwd"}`,
							},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": "done"},
			}},
		})
	}))
	captured := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
	return srv, captured
}

func newAutoModeAgent(t *testing.T, llmURL string, eval automode.Evaluator) (*sysagent.Agent, *captureClient, *atomic.Bool) {
	t.Helper()
	var ran atomic.Bool
	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name:        "mutate_thing",
		Description: "mutating test tool",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Mutating:    true,
		Call: func(context.Context, tools.Actor, json.RawMessage) (string, error) {
			ran.Store(true)
			return "mutated", nil
		},
	})
	agent := sysagent.New(sysagent.Deps{
		LLM: sysagent.LLMConfig{
			BaseURL: llmURL,
			APIKey:  "k",
			Model:   "m",
		},
		Tools:     reg,
		Actor:     tools.Actor{Username: "u"},
		Evaluator: eval,
	})
	return agent, &captureClient{}, &ran
}

func runOneTurn(t *testing.T, agent *sysagent.Agent, client acp.Client, autoMode bool) {
	t.Helper()
	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()
	asc := acp.NewAgentSideConnection(agent, a2cW, c2aR)
	agent.SetAgentConnection(asc)
	csc := acp.NewClientSideConnection(client, c2aW, a2cR)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := csc.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber}); err != nil {
		t.Fatal(err)
	}
	sess, err := csc.NewSession(ctx, acp.NewSessionRequest{Cwd: ".", McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	if autoMode {
		agent.SetAutoMode(string(sess.SessionId), true)
	}
	if _, err := csc.Prompt(ctx, acp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock("please mutate things")},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAgent_AutoModeAllowsWithoutPrompt(t *testing.T) {
	llm, bodies := autoModeLLM(t)
	defer llm.Close()
	eval := &stubEvaluator{verdict: automode.Verdict{Decision: automode.Allow, Rule: "routine"}}
	agent, client, ran := newAutoModeAgent(t, llm.URL, eval)

	runOneTurn(t, agent, client, true)

	if !ran.Load() {
		t.Fatal("allowed tool should have run")
	}
	if n := client.permissionRequests(); n != 0 {
		t.Fatalf("auto mode must not open a permission dialog, got %d", n)
	}
	req := eval.lastRequest()
	if req.Name != "mutate_thing" || !strings.Contains(req.Args, "/etc/passwd") {
		t.Fatalf("evaluator request missing call details: %+v", req)
	}
	if !strings.Contains(req.UserDigest, "please mutate things") {
		t.Fatalf("evaluator request missing conversation digest: %q", req.UserDigest)
	}
	_ = bodies()
}

func TestAgent_AutoModeBlocksWithReason(t *testing.T) {
	llm, bodies := autoModeLLM(t)
	defer llm.Close()
	eval := &stubEvaluator{verdict: automode.Verdict{
		Decision: automode.SoftDeny,
		Rule:     "Outside Workspace",
		Reason:   "target is not inside the workspace",
	}}
	agent, client, ran := newAutoModeAgent(t, llm.URL, eval)

	runOneTurn(t, agent, client, true)

	if ran.Load() {
		t.Fatal("blocked tool must not run")
	}
	if n := client.permissionRequests(); n != 0 {
		t.Fatalf("auto mode must not open a permission dialog, got %d", n)
	}
	updates := client.toolStatuses()
	if len(updates) == 0 || updates[len(updates)-1].status != string(acp.ToolCallStatusFailed) {
		t.Fatalf("blocked call should finish failed, got %+v", updates)
	}
	reqs := bodies()
	if len(reqs) < 2 {
		t.Fatalf("expected a second LLM round, got %d requests", len(reqs))
	}
	second := reqs[len(reqs)-1]
	if !strings.Contains(second, "Blocked by auto mode") ||
		!strings.Contains(second, "Outside Workspace") ||
		!strings.Contains(second, "target is not inside the workspace") {
		t.Fatalf("tool result must carry rule and reason, got %s", second)
	}
}

func TestAgent_AutoModeOffKeepsDialog(t *testing.T) {
	llm, _ := autoModeLLM(t)
	defer llm.Close()
	agent, client, ran := newAutoModeAgent(t, llm.URL, nil)

	runOneTurn(t, agent, client, false)

	if !ran.Load() {
		t.Fatal("approved tool should have run")
	}
	if n := client.permissionRequests(); n != 1 {
		t.Fatalf("auto off should ask the user once, got %d", n)
	}
}

// questionClient answers permission requests with the first option and records
// the request so tests can assert the meta contract over the ACP pipe.
type questionClient struct {
	captureClient
	mu      sync.Mutex
	meta    map[string]any
	options []acp.PermissionOption
}

func (c *questionClient) RequestPermission(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.meta = req.Meta
	c.options = req.Options
	c.mu.Unlock()
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: "opt-0"},
		},
	}, nil
}

func TestAgent_AskUserQuestionCarriesQuestionMeta(t *testing.T) {
	var round atomic.Int32
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if round.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"finish_reason": "tool_calls",
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      "AskUserQuestion",
								"arguments": `{"question":"Which env?","options":[{"label":"A"},{"label":"B"}]}`,
							},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": "done"},
			}},
		})
	}))
	defer llm.Close()

	reg := tools.NewRegistry()
	tools.RegisterInteractive(reg)
	agent := sysagent.New(sysagent.Deps{
		LLM:   sysagent.LLMConfig{BaseURL: llm.URL, APIKey: "k", Model: "m"},
		Tools: reg,
		Actor: tools.Actor{Username: "u"},
	})
	client := &questionClient{}
	runOneTurn(t, agent, client, false)

	client.mu.Lock()
	meta := client.meta
	options := client.options
	client.mu.Unlock()
	if meta[sysagent.PermissionMetaKindKey] != sysagent.PermissionKindQuestion {
		t.Fatalf("question meta did not survive the ACP pipe: %+v", meta)
	}
	if len(options) == 0 || options[0].Kind != acp.PermissionOptionKindAllowOnce {
		t.Fatalf("question options changed: %+v", options)
	}
}

func TestAgent_AutoModeFailClosedOnEvaluatorError(t *testing.T) {
	llm, bodies := autoModeLLM(t)
	defer llm.Close()
	eval := &stubEvaluator{
		verdict: automode.Verdict{Decision: automode.SoftDeny, Rule: "classifier_unavailable", Reason: "boom"},
		err:     context.DeadlineExceeded,
	}
	agent, client, ran := newAutoModeAgent(t, llm.URL, eval)

	runOneTurn(t, agent, client, true)

	if ran.Load() {
		t.Fatal("evaluator failure must block the call")
	}
	reqs := bodies()
	if len(reqs) < 2 || !strings.Contains(reqs[len(reqs)-1], "classifier_unavailable") {
		t.Fatalf("failure reason must reach the model, got %v", reqs)
	}
	if n := client.permissionRequests(); n != 0 {
		t.Fatalf("failed classification must not fall back to a dialog, got %d", n)
	}
}
