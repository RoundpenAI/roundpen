package sysagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func TestAgent_ParallelSafeToolsOverlap(t *testing.T) {
	var mu sync.Mutex
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		round++
		n := round
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		if n == 1 {
			events := []string{
				`{"choices":[{"delta":{"role":"assistant"}}]}`,
				`{"choices":[{"delta":{"content":"Checking three files."}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"slow_a","arguments":"{}"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"slow_b","arguments":"{}"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":2,"id":"c3","type":"function","function":{"name":"slow_c","arguments":"{}"}}]}}]}`,
				`{"choices":[{"finish_reason":"tool_calls","delta":{}}]}`,
			}
			for _, e := range events {
				fmt.Fprintf(w, "data: %s\n\n", e)
				fl.Flush()
			}
		} else {
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"all done"}}]}`)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer srv.Close()

	var inflight atomic.Int32
	var maxInflight atomic.Int32
	slow := func(name string) tools.Tool {
		return tools.Tool{
			Name:        name,
			Description: name,
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Mutating:    false,
			Call: func(context.Context, tools.Actor, json.RawMessage) (string, error) {
				n := inflight.Add(1)
				for {
					cur := maxInflight.Load()
					if n <= cur || maxInflight.CompareAndSwap(cur, n) {
						break
					}
				}
				time.Sleep(120 * time.Millisecond)
				inflight.Add(-1)
				return name + "-ok", nil
			},
		}
	}

	reg := tools.NewRegistry()
	reg.Register(slow("slow_a"))
	reg.Register(slow("slow_b"))
	reg.Register(slow("slow_c"))

	agent := sysagent.New(sysagent.Deps{
		LLM:   sysagent.LLMConfig{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTPClient: srv.Client()},
		Tools: reg,
		Actor: tools.Actor{Username: "u"},
	})
	client := &thoughtRecordingClient{captureClient: &captureClient{}}
	start := time.Now()
	_ = driveAgent(t, agent, client, "read three")
	elapsed := time.Since(start)

	if maxInflight.Load() < 2 {
		t.Fatalf("expected overlapping tool invokes, max inflight=%d", maxInflight.Load())
	}
	// Three 120ms serial tools would be ~360ms+; parallel should land well under that.
	if elapsed > 350*time.Millisecond {
		t.Fatalf("expected parallel wall clock, elapsed=%s (max inflight=%d)", elapsed, maxInflight.Load())
	}

	texts := client.textsSnapshot()
	joined := strings.Join(texts, "")
	if !strings.Contains(joined, "Checking three files.") {
		t.Fatalf("expected streamed preamble before tools, got %q", texts)
	}
	if !strings.Contains(joined, "all done") {
		t.Fatalf("expected final reply after tools, got %q", texts)
	}

	client.mu.Lock()
	thoughts := append([]string(nil), client.thoughts...)
	client.mu.Unlock()
	thoughtJoin := strings.Join(thoughts, "")
	if !strings.Contains(thoughtJoin, "parallel") {
		t.Fatalf("expected parallel progress thought, got %q", thoughts)
	}

	statuses := client.toolStatuses()
	var sawInProgress, completed int
	for _, s := range statuses {
		if s.status == "in_progress" {
			sawInProgress++
		}
		if s.status == "completed" {
			completed++
		}
	}
	if sawInProgress < 3 {
		t.Fatalf("expected in_progress for each tool, statuses=%+v", statuses)
	}
	if completed < 3 {
		t.Fatalf("expected 3 completed tools, statuses=%+v", statuses)
	}
}

func TestToolParallelSafe(t *testing.T) {
	cases := []struct {
		name     string
		mutating bool
		want     bool
	}{
		{"Read", false, true},
		{"WebSearch", false, true},
		{"Write", true, false},
		{"Bash", true, false},
		{"Skill", false, false},
		{"browser_snapshot", false, false},
	}
	for _, tc := range cases {
		if got := sysagent.ToolParallelSafeForTest(tc.name, tc.mutating); got != tc.want {
			t.Fatalf("%s mutating=%v: got %v want %v", tc.name, tc.mutating, got, tc.want)
		}
	}
}
