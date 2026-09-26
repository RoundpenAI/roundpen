package sysagent_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

// TestAgent_SteerInjectsIntoTurn: a steered message must reach the running
// turn's next LLM request as a user row — no cancellation, no second turn.
func TestAgent_SteerInjectsIntoTurn(t *testing.T) {
	var mu sync.Mutex
	var requests [][]map[string]any

	release := make(chan struct{})
	started := make(chan struct{})

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		requests = append(requests, req.Messages)
		n := len(requests)
		mu.Unlock()
		if n == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"finish_reason": "tool_calls",
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"id":   "call_wait",
							"type": "function",
							"function": map[string]any{
								"name":      "wait",
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
				"message":       map[string]any{"role": "assistant", "content": "done"},
			}},
		})
	}))
	defer llm.Close()

	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name:        "wait",
		Description: "blocks until the test releases it",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(ctx context.Context, _ tools.Actor, _ json.RawMessage) (string, error) {
			close(started)
			select {
			case <-release:
				return "released", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
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
	sid := string(sess.SessionId)

	// Idle session: nothing to steer into.
	if err := agent.SteerSession(sid, "too early"); !errors.Is(err, sysagent.ErrNoActiveTurn) {
		t.Fatalf("steer while idle: err=%v", err)
	}

	type promptResult struct {
		resp acp.PromptResponse
		err  error
	}
	promptDone := make(chan promptResult, 1)
	go func() {
		resp, err := csc.Prompt(ctx, acp.PromptRequest{
			SessionId: sess.SessionId,
			Prompt:    []acp.ContentBlock{acp.TextBlock("run the wait tool")},
		})
		promptDone <- promptResult{resp, err}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("tool never started")
	}

	if err := agent.SteerSession(sid, "STEER-MARKER"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	close(release)

	res := <-promptDone
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.resp.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("stop reason = %q", res.resp.StopReason)
	}

	mu.Lock()
	got := requests
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("llm requests = %d, want 2", len(got))
	}
	found := false
	for _, m := range got[1] {
		if m["role"] == "user" && m["content"] == "STEER-MARKER" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steered message missing from second request: %#v", got[1])
	}

	// The turn is over: the cancel slot must be cleared again.
	if err := agent.SteerSession(sid, "after"); !errors.Is(err, sysagent.ErrNoActiveTurn) {
		t.Fatalf("steer after turn: err=%v", err)
	}
}

// TestAgent_SteerDuringFinalTextRestartsTheLoop: a steer that lands right as
// the model finishes its text must not be dropped — the turn folds it in and
// asks the model again instead of ending.
func TestAgent_SteerDuringFinalTextRestartsTheLoop(t *testing.T) {
	var mu sync.Mutex
	var requests [][]map[string]any

	steerSent := make(chan struct{})

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		requests = append(requests, req.Messages)
		n := len(requests)
		mu.Unlock()
		if n == 1 {
			// Stall the first request until the steer has been delivered, then
			// answer with plain text so the turn reaches its end naturally.
			select {
			case <-steerSent:
			case <-time.After(5 * time.Second):
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"finish_reason": "stop",
					"message":       map[string]any{"role": "assistant", "content": "first part"},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": " extended"},
			}},
		})
	}))
	defer llm.Close()

	reg := tools.NewRegistry()
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
	sid := string(sess.SessionId)

	promptDone := make(chan error, 1)
	go func() {
		_, err := csc.Prompt(ctx, acp.PromptRequest{
			SessionId: sess.SessionId,
			Prompt:    []acp.ContentBlock{acp.TextBlock("hi")},
		})
		promptDone <- err
	}()

	// Wait until the first LLM request is in flight, then steer and let it end.
	waitForRequests(t, &mu, &requests, 1)
	if err := agent.SteerSession(sid, "STEER-LATE"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	close(steerSent)

	if err := <-promptDone; err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	got := requests
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("llm requests = %d, want 2 (the late steer must restart the loop)", len(got))
	}
	found := false
	for _, m := range got[1] {
		if m["role"] == "user" && m["content"] == "STEER-LATE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("late steer missing from second request: %#v", got[1])
	}
}

func waitForRequests(t *testing.T, mu *sync.Mutex, requests *[][]map[string]any, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*requests)
		mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d llm request(s)", want)
}
