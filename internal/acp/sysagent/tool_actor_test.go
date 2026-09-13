package sysagent_test

import (
	"context"
	"encoding/json"
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
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// slotStub mirrors sandbox.Manager's owner-scoped contract: calls without an
// actor in ctx fail exactly like requireActor does.
type slotStub struct {
	actor  authz.Actor
	called bool
	mu     sync.Mutex
}

func (s *slotStub) EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error) {
	a, ok := authz.From(ctx)
	if !ok {
		return nil, sandbox.ErrUnauthorized
	}
	s.mu.Lock()
	s.actor, s.called = a, true
	s.mu.Unlock()
	return &sandbox.Sandbox{ID: "sb-agent", Status: sandbox.StatusRunning}, nil
}

type execStub struct {
	cmd []string
	mu  sync.Mutex
}

func (e *execStub) Exec(ctx context.Context, id string, req sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	if _, ok := authz.From(ctx); !ok {
		return nil, sandbox.ErrUnauthorized
	}
	e.mu.Lock()
	e.cmd = append([]string(nil), req.Cmd...)
	e.mu.Unlock()
	return &sandbox.ExecResult{ExitCode: 0, Stdout: []byte("total 0\n")}, nil
}

func (e *execStub) WorkspaceHostPath(ctx context.Context, id string) (string, error) {
	if _, ok := authz.From(ctx); !ok {
		return "", sandbox.ErrUnauthorized
	}
	return "", nil
}

// TestAgent_ToolCallsCarryActor drives the agent over pipes like production and
// checks that Bash reaches the sandbox manager with the session actor attached,
// the way HTTP middleware attaches it for API requests.
func TestAgent_ToolCallsCarryActor(t *testing.T) {
	var mu sync.Mutex
	round := 0
	sawToolResult := ""
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		round++
		n := round
		if n > 1 {
			sawToolResult = string(body)
		}
		mu.Unlock()
		if n == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"finish_reason": "tool_calls",
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      "Bash",
								"arguments": `{"command":"ls -la /workspace"}`,
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

	slots := &slotStub{}
	ex := &execStub{}
	reg := tools.NewRegistry()
	tools.RegisterShell(reg, &tools.AgentBinder{Slots: slots, Exec: ex})

	agent := sysagent.New(sysagent.Deps{
		LLM: sysagent.LLMConfig{
			BaseURL:    llm.URL,
			APIKey:     "k",
			Model:      "m",
			HTTPClient: llm.Client(),
		},
		Tools: reg,
		Actor: tools.Actor{Username: "alice", Role: "user"},
	})

	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()
	asc := acp.NewAgentSideConnection(agent, a2cW, c2aR)
	agent.SetAgentConnection(asc)
	csc := acp.NewClientSideConnection(&captureClient{}, c2aW, a2cR)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := csc.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber}); err != nil {
		t.Fatal(err)
	}
	sess, err := csc.NewSession(ctx, acp.NewSessionRequest{Cwd: ".", McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := csc.Prompt(ctx, acp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock("list the workspace")},
	}); err != nil {
		t.Fatal(err)
	}

	slots.mu.Lock()
	called, actor := slots.called, slots.actor
	slots.mu.Unlock()
	if !called {
		t.Fatalf("agent slot never saw an authz actor; llm saw: %s", sawToolResult)
	}
	if want := (authz.Actor{Username: "alice"}); actor != want {
		t.Fatalf("actor = %#v, want %#v", actor, want)
	}
	ex.mu.Lock()
	cmd := ex.cmd
	ex.mu.Unlock()
	if len(cmd) != 3 || cmd[2] != "ls -la /workspace" {
		t.Fatalf("exec cmd = %q", cmd)
	}
	if strings.Contains(sawToolResult, "unauthorized") {
		t.Fatalf("tool result leaked unauthorized: %s", sawToolResult)
	}
	if !strings.Contains(sawToolResult, "exitCode") {
		t.Fatalf("tool result missing exec payload: %s", sawToolResult)
	}
}
