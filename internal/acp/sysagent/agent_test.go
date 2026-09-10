package sysagent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
)

type memHistory struct {
	rows []*agentsession.Message
}

func (m memHistory) ListMessages(context.Context, string, int) ([]*agentsession.Message, error) {
	return m.rows, nil
}

type captureClient struct {
	texts []string
}

func (c *captureClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	allow := acp.PermissionOptionId("allow")
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: allow},
		},
	}, nil
}
func (c *captureClient) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	if n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil {
		c.texts = append(c.texts, n.Update.AgentMessageChunk.Content.Text.Text)
	}
	return nil
}
func (c *captureClient) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, nil
}
func (c *captureClient) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, nil
}
func (c *captureClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound("terminal")
}
func (c *captureClient) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, nil
}
func (c *captureClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, nil
}
func (c *captureClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, nil
}
func (c *captureClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, nil
}

func TestAgent_ToolLoop(t *testing.T) {
	var round atomic.Int32
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := round.Add(1)
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
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
								"name":      "ping",
								"arguments": `{}`,
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
				"message":       map[string]any{"role": "assistant", "content": "pong done"},
			}},
		})
	}))
	defer llm.Close()

	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name:        "ping",
		Description: "ping",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(context.Context, tools.Actor, json.RawMessage) (string, error) {
			return `{"pong":true}`, nil
		},
	})

	agent := sysagent.New(sysagent.Deps{
		LLM: sysagent.LLMConfig{
			BaseURL:    llm.URL,
			APIKey:     "k",
			Model:      "m",
			HTTPClient: llm.Client(),
		},
		Tools: reg,
		Actor: tools.Actor{Username: "u"},
	})

	// Drive over pipes like production.
	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()
	client := &captureClient{}
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
	resp, err := csc.Prompt(ctx, acp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock("ping please")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason == "" {
		t.Fatal("empty stop")
	}
	found := false
	for _, ttxt := range client.texts {
		if strings.Contains(ttxt, "pong done") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected final text, got %#v", client.texts)
	}
	if round.Load() < 2 {
		t.Fatalf("expected 2 llm rounds, got %d", round.Load())
	}
}

func TestAgent_ReplaysPersistedHistory(t *testing.T) {
	var sawPrior atomic.Bool
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "yesterday question") && strings.Contains(string(body), "try again") {
			sawPrior.Store(true)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer llm.Close()

	agent := sysagent.New(sysagent.Deps{
		LLM: sysagent.LLMConfig{
			BaseURL:    llm.URL,
			APIKey:     "k",
			Model:      "m",
			HTTPClient: llm.Client(),
		},
		SessionID: "sess-db",
		History: memHistory{rows: []*agentsession.Message{
			{Role: agentsession.RoleUser, Content: "yesterday question"},
			{Role: agentsession.RoleAssistant, Content: "yesterday answer"},
			{Role: agentsession.RoleUser, Content: "try again"},
		}},
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
		Prompt:    []acp.ContentBlock{acp.TextBlock("try again")},
	}); err != nil {
		t.Fatal(err)
	}
	if !sawPrior.Load() {
		t.Fatal("expected persisted history in the LLM request")
	}
}

func TestLoopbackBase(t *testing.T) {
	if got := sysagent.LoopbackBase(":19001"); got != "http://127.0.0.1:19001" {
		t.Fatalf("got %q", got)
	}
	if got := sysagent.LoopbackBase("0.0.0.0:19001"); got != "http://127.0.0.1:19001" {
		t.Fatalf("got %q", got)
	}
	if got := sysagent.LoopbackBase("[::]:19001"); got != "http://127.0.0.1:19001" {
		t.Fatalf("got %q", got)
	}
}
