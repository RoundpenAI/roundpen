package imconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"

	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func steerTestAgent(t *testing.T, llmURL string) *Agent {
	t.Helper()
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatal(err)
	}
	user := fmt.Sprintf("im-steer-%d", time.Now().UnixNano())
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO users (username, api_key, role) VALUES ($1,$1,'user')`, user); err != nil {
		t.Fatal(err)
	}
	mgr := manager.New(nil, nil, providers.Default(), manager.SysDeps{
		LLM: func(string) sysagent.LLMConfig {
			return sysagent.LLMConfig{BaseURL: llmURL + "/llmgw/openai", APIKey: "k", Model: "m"}
		},
	})
	return &Agent{
		Store:      &agentsession.Store{DB: db.SQL},
		ACP:        mgr,
		ProviderID: "sysadmin",
		UserID:     user,
		Role:       "user",
		APIKey:     "k",
	}
}

func drainToResult(t *testing.T, s core.AgentSession) core.Event {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == core.EventError {
				t.Fatalf("turn error: %v", ev.Error)
			}
			if ev.Type == core.EventResult {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for a terminal event")
			return core.Event{}
		}
	}
}

// TestAgentSessionSendSteersWhileBusy: a message arriving mid-turn (the
// cc-connect /ps path) is injected into the running turn, not queued.
func TestAgentSessionSendSteersWhileBusy(t *testing.T) {
	var mu sync.Mutex
	var requests [][]map[string]any
	gate := make(chan struct{})
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
			close(started)
			<-gate
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer llm.Close()

	a := steerTestAgent(t, llm.URL)
	ctx := context.Background()
	s, err := a.StartSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Send("first", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first llm request never started")
	}

	if err := s.Send("second", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	as, ok := s.(*agentSession)
	if !ok {
		t.Fatalf("unexpected session type %T", s)
	}
	as.mu.Lock()
	pending := append([]string(nil), as.pending...)
	as.mu.Unlock()
	if len(pending) != 0 {
		t.Fatalf("mid-turn send must steer, not queue: %+v", pending)
	}

	close(gate)
	drainToResult(t, s)

	mu.Lock()
	got := requests
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("llm requests = %d, want 2", len(got))
	}
	found := false
	for _, m := range got[1] {
		if m["role"] == "user" && m["content"] == "second" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steered message missing from the follow-up request: %#v", got[1])
	}
}

// TestAgentSessionCancelTurnKeepsSessionAlive: the engine's /stop maps onto
// CancelTurn, which interrupts the turn without tearing the session down.
func TestAgentSessionCancelTurnKeepsSessionAlive(t *testing.T) {
	started := make(chan struct{})
	var canceled atomic.Bool
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		// Drain the body first: only then does net/http watch the connection
		// for close, which is what cancels r.Context().
		_, _ = io.ReadAll(r.Body)
		<-r.Context().Done() // hang until the agent aborts the request
		canceled.Store(true)
	}))
	t.Cleanup(func() {
		llm.CloseClientConnections()
		llm.Close()
	})

	a := steerTestAgent(t, llm.URL)
	ctx := context.Background()
	s, err := a.StartSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	as, ok := s.(*agentSession)
	if !ok {
		t.Fatalf("unexpected session type %T", s)
	}
	if _, ok := interface{}(as).(core.AgentSessionCanceller); !ok {
		t.Fatal("agentSession must satisfy core.AgentSessionCanceller (engine /stop path)")
	}

	if err := s.Send("long running", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("llm request never started")
	}

	if err := as.CancelTurn(); err != nil {
		t.Fatalf("CancelTurn: %v", err)
	}
	drainToResult(t, s)

	if !as.Alive() {
		t.Fatal("CancelTurn must keep the session alive")
	}
	if _, ok := a.ACP.Get(as.sess.ID); !ok {
		t.Fatal("CancelTurn must keep the runtime attached")
	}
	waitUntil(t, 5*time.Second, canceled.Load, "the aborted llm request to be cancelled client-side")
}

func waitUntil(t *testing.T, d time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
