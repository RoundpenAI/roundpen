package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
)

// queueLLM is a minimal OpenAI-compatible stub that answers every request.
func queueLLM(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// queueTestRunner is commandTestRunner plus a live in-process runtime, so the
// queue and steer paths exercise a real manager.Runtime.
func queueTestRunner(t *testing.T, llmURL string) (*runner, *agentsession.Store) {
	t.Helper()
	ctx := context.Background()
	store, db := commandTestStore(t)
	user := fmt.Sprintf("queue-test-%d", time.Now().UnixNano())
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO users (username, api_key, role) VALUES ($1,$1,'user')`, user); err != nil {
		t.Fatal(err)
	}
	sess, err := store.Create(ctx, user, "queue test", "sysadmin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sys := manager.SysDeps{}
	if llmURL != "" {
		sys.LLM = func(string) sysagent.LLMConfig {
			return sysagent.LLMConfig{BaseURL: llmURL + "/llmgw/openai", APIKey: "k", Model: "m"}
		}
	}
	mgr := manager.New(nil, nil, providers.Default(), sys)
	actor := manager.Actor{Username: user, Role: "user", APIKey: "k"}
	rt, err := mgr.Start(ctx, sess.ID, "", "sysadmin", manager.StartOpts{Actor: actor})
	if err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(sess.ID) })
	r := &runner{
		handler: &Handler{Store: store, ACP: mgr},
		session: sess,
		acp:     mgr,
		rt:      rt,
		actor:   actor,
		ctx:     ctx,
		cancel:  func() {},
		clients: map[*wsClient]struct{}{},
		auto:    true,
		perms:   map[string]*permWait{},
	}
	return r, store
}

func (r *runner) pendingSnapshot() []pendingItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]pendingItem(nil), r.pending...)
}

func setBusy(r *runner, v bool) {
	r.mu.Lock()
	r.busy = v
	r.mu.Unlock()
}

// TestRunnerBusyPromptQueuesAndDrains: prompts sent while busy queue in order
// and run one after another, with the queue shrinking at each hand-off.
func TestRunnerBusyPromptQueuesAndDrains(t *testing.T) {
	llm := queueLLM(t)
	r, _ := queueTestRunner(t, llm.URL)

	setBusy(r, true)
	r.prompt("first", "c-1")
	r.prompt("second", "c-2")

	got := r.pendingSnapshot()
	if len(got) != 2 || got[0].Text != "first" || got[0].ClientMsgID != "c-1" || got[1].Text != "second" {
		t.Fatalf("queue: %+v", got)
	}
	if got[0].ID == "" || got[1].ID == "" {
		t.Fatalf("queued items must carry their persisted row id: %+v", got)
	}
	rows := r.testRows(t)
	if len(rows) != 2 || rows[0].Role != agentsession.RoleUser || rows[0].Content != "first" {
		t.Fatalf("queued rows persist before running: %+v", rows)
	}

	// The in-flight turn ends: the runner must pick the first item up.
	r.finishTurn()
	waitFor(t, "first queued turn to finish", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return !r.busy && len(r.pending) == 0
	})
}

// TestRunnerSteerFallsBackToQueueWhenNoActiveTurn: a steer with a live
// runtime but no running turn queues the message instead of dropping it.
func TestRunnerSteerFallsBackToQueueWhenNoActiveTurn(t *testing.T) {
	llm := queueLLM(t)
	r, _ := queueTestRunner(t, llm.URL)

	setBusy(r, true) // runner believes a turn is up; the ACP side is idle
	r.steer("steered text", "c-steer")

	got := r.pendingSnapshot()
	if len(got) != 1 || got[0].Text != "steered text" || got[0].ClientMsgID != "c-steer" {
		t.Fatalf("steer must queue when no turn is live: %+v", got)
	}
	rows := r.testRows(t)
	if len(rows) != 1 || rows[0].Content != "steered text" {
		t.Fatalf("steered row must be persisted: %+v", rows)
	}
}

// TestRunnerUnqueueCancelsRow: pulling a queued message back removes it from
// the queue and marks the persisted row cancelled.
func TestRunnerUnqueueCancelsRow(t *testing.T) {
	llm := queueLLM(t)
	r, _ := queueTestRunner(t, llm.URL)

	setBusy(r, true)
	r.prompt("pull me back", "c-x")
	got := r.pendingSnapshot()
	if len(got) != 1 {
		t.Fatalf("queue: %+v", got)
	}

	r.unqueue(got[0].ID, &wsClient{closed: true})

	if left := r.pendingSnapshot(); len(left) != 0 {
		t.Fatalf("queue after pull-back: %+v", left)
	}
	rows := r.testRows(t)
	if len(rows) != 1 || !agentsession.IsCancelled(rows[0]) {
		t.Fatalf("row must be marked cancelled: %+v", rows)
	}
}

// TestRunnerUnqueueDrainedMessageIsRefused: once a message left the queue the
// pull-back no longer touches it (the client gets an error frame instead).
func TestRunnerUnqueueDrainedMessageIsRefused(t *testing.T) {
	llm := queueLLM(t)
	r, _ := queueTestRunner(t, llm.URL)

	setBusy(r, true)
	r.prompt("already running", "c-run")
	got := r.pendingSnapshot()

	// Simulate the drain hand-off: take it out of the queue as finishTurn would.
	r.mu.Lock()
	r.pending = nil
	r.mu.Unlock()

	r.unqueue(got[0].ID, &wsClient{closed: true})

	rows := r.testRows(t)
	if len(rows) != 1 || agentsession.IsCancelled(rows[0]) {
		t.Fatalf("drained row must not be marked cancelled: %+v", rows)
	}
}

// TestRunnerClearCancelsQueuedRows: /clear abandons the queue and marks every
// queued row cancelled so the model never sees them.
func TestRunnerClearCancelsQueuedRows(t *testing.T) {
	llm := queueLLM(t)
	r, _ := queueTestRunner(t, llm.URL)

	setBusy(r, true)
	r.prompt("doomed one", "c-1")
	r.prompt("doomed two", "c-2")

	r.command("clear", "")

	if left := r.pendingSnapshot(); len(left) != 0 {
		t.Fatalf("clear must empty the queue: %+v", left)
	}
	rows := r.testRows(t)
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	for _, m := range rows {
		if !agentsession.IsCancelled(m) {
			t.Fatalf("queued row must be cancelled by /clear: %+v", m)
		}
	}
}
