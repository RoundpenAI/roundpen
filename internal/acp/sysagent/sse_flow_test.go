package sysagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

// thoughtRecordingClient records agent-thought chunks in addition to the base
// capture behavior.
type thoughtRecordingClient struct {
	*captureClient
	mu       sync.Mutex
	thoughts []string
}

func (c *thoughtRecordingClient) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	if n.Update.AgentThoughtChunk != nil && n.Update.AgentThoughtChunk.Content.Text != nil {
		c.mu.Lock()
		c.thoughts = append(c.thoughts, n.Update.AgentThoughtChunk.Content.Text.Text)
		c.mu.Unlock()
	}
	return c.captureClient.SessionUpdate(ctx, n)
}

func sseServer(events []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
}

func TestAgent_SSEStreamWithReasoningAndToolCalls(t *testing.T) {
	var mu sync.Mutex
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		round++
		n := round
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		events := []string{
			`{"choices":[{"delta":{"content":"pong done"}}]}`,
		}
		if n == 1 {
			events = []string{
				`{"choices":[{"delta":{"role":"assistant"}}]}`,
				`{"choices":[{"delta":{"reasoning_content":"pondering "}}]}`,
				`{"choices":[{"delta":{"reasoning":"the request"}}]}`,
				`{"choices":[{"delta":{"content":"let me ping"}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"ping","arguments":"{}"}}]}}]}`,
				`{"choices":[{"finish_reason":"tool_calls","delta":{}}]}`,
			}
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer srv.Close()

	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name:        "ping",
		Description: "ping",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(context.Context, tools.Actor, json.RawMessage) (string, error) {
			return "pong", nil
		},
	})
	agent := sysagent.New(sysagent.Deps{
		LLM:   sysagent.LLMConfig{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTPClient: srv.Client()},
		Tools: reg,
		Actor: tools.Actor{Username: "u"},
	})

	client := &thoughtRecordingClient{captureClient: &captureClient{}}
	resp := driveAgent(t, agent, client, "ping please")
	if string(resp.StopReason) == "" {
		t.Fatal("empty stop reason")
	}

	client.mu.Lock()
	thoughts := append([]string(nil), client.thoughts...)
	client.mu.Unlock()
	texts := client.textsSnapshot()

	if joined := strings.Join(thoughts, ""); !strings.Contains(joined, "pondering the request") {
		t.Fatalf("expected reasoning chunks surfaced as thoughts, got %q", thoughts)
	}
	if !strings.Contains(strings.Join(texts, ""), "let me ping") {
		t.Fatalf("expected streamed content, got %q", texts)
	}
	if !strings.Contains(strings.Join(texts, ""), "pong done") {
		t.Fatalf("expected final text, got %q", texts)
	}
	statuses := client.toolStatuses()
	if len(statuses) != 1 || statuses[0].status != "completed" {
		t.Fatalf("expected one completed ping call, got %+v", statuses)
	}
}

func TestAgent_SSEStreamErrorChunk(t *testing.T) {
	srv := sseServer([]string{
		`{"choices":[{"delta":{"content":"partial..."}}]}`,
		`{"error":{"message":"model overloaded"}}`,
	})
	defer srv.Close()

	agent := sysagent.New(sysagent.Deps{
		LLM:   sysagent.LLMConfig{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTPClient: srv.Client()},
		Tools: tools.NewRegistry(),
		Actor: tools.Actor{Username: "u"},
	})
	client := &thoughtRecordingClient{captureClient: &captureClient{}}
	resp, err := driveAgentE(t, agent, client, "hello")
	if err == nil {
		t.Fatalf("expected the stream error to surface, got stop=%q", resp.StopReason)
	}
	if !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("err = %v", err)
	}
}
