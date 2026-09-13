package sysagent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

// TestAgent_CancelResolvesToolCall: after the user stops a turn, the in-flight
// tool call must still receive a terminal update, or the UI keeps spinning.
func TestAgent_CancelResolvesToolCall(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 && json.Valid(body) {
			var req struct {
				Messages []struct {
					Role string `json:"role"`
				} `json:"messages"`
			}
			_ = json.Unmarshal(body, &req)
			for _, m := range req.Messages {
				if m.Role == "tool" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"choices": []map[string]any{{
							"finish_reason": "stop",
							"message":       map[string]any{"role": "assistant", "content": "done"},
						}},
					})
					return
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{{
						"id":   "call_slow",
						"type": "function",
						"function": map[string]any{
							"name":      "slow",
							"arguments": `{}`,
						},
					}},
				},
			}},
		})
	}))
	defer llm.Close()

	started := make(chan struct{})
	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name:        "slow",
		Description: "blocks until the turn is cancelled",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(ctx context.Context, _ tools.Actor, _ json.RawMessage) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
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
		Actor: tools.Actor{Username: "alice"},
	})

	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()
	asc := acp.NewAgentSideConnection(agent, a2cW, c2aR)
	agent.SetAgentConnection(asc)
	client := &captureClient{}
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

	promptErr := make(chan error, 1)
	go func() {
		_, err := csc.Prompt(ctx, acp.PromptRequest{
			SessionId: sess.SessionId,
			Prompt:    []acp.ContentBlock{acp.TextBlock("run the slow tool")},
		})
		promptErr <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("slow tool never started")
	}
	if err := csc.Cancel(ctx, acp.CancelNotification{SessionId: sess.SessionId}); err != nil {
		t.Fatal(err)
	}
	if err := <-promptErr; err != nil {
		t.Fatal(err)
	}

	terminal := false
	for _, u := range client.toolStatuses() {
		if u.id == "call_slow" && (u.status == "failed" || u.status == "completed") {
			terminal = true
		}
	}
	if !terminal {
		t.Fatalf("no terminal tool update after cancel: %#v", client.toolStatuses())
	}
}

// blockingPermClient never answers permission requests; cancelling the turn
// cancels the outstanding request instead.
type blockingPermClient struct {
	*captureClient
}

func (b *blockingPermClient) RequestPermission(ctx context.Context, _ acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	<-ctx.Done()
	return acp.RequestPermissionResponse{}, ctx.Err()
}

// TestAgent_CancelDuringPermissionResolvesToolCall: a tool call that is waiting
// on the permission dialog must also reach a terminal state when the turn is
// cancelled.
func TestAgent_CancelDuringPermissionResolvesToolCall(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{{
						"id":   "call_perm",
						"type": "function",
						"function": map[string]any{
							"name":      "mutating",
							"arguments": `{}`,
						},
					}},
				},
			}},
		})
	}))
	defer llm.Close()

	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name:        "mutating",
		Description: "needs permission",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Mutating:    true,
		Call: func(context.Context, tools.Actor, json.RawMessage) (string, error) {
			return "ok", nil
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
		Actor: tools.Actor{Username: "alice"},
	})

	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()
	asc := acp.NewAgentSideConnection(agent, a2cW, c2aR)
	agent.SetAgentConnection(asc)
	client := &blockingPermClient{captureClient: &captureClient{}}
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

	promptErr := make(chan error, 1)
	go func() {
		_, err := csc.Prompt(ctx, acp.PromptRequest{
			SessionId: sess.SessionId,
			Prompt:    []acp.ContentBlock{acp.TextBlock("run the mutating tool")},
		})
		promptErr <- err
	}()

	// Give the agent time to reach the permission request, then stop the turn.
	time.Sleep(200 * time.Millisecond)
	if err := csc.Cancel(ctx, acp.CancelNotification{SessionId: sess.SessionId}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-promptErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not return after cancel")
	}

	for _, u := range client.toolStatuses() {
		if u.id == "call_perm" && (u.status == "failed" || u.status == "completed") {
			return
		}
	}
	t.Fatalf("no terminal tool update after cancel during permission: %#v", client.toolStatuses())
}
