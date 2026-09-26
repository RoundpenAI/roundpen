package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

type stubEvaluator struct {
	verdict automode.Verdict
	err     error
	calls   int
	last    automode.Request
}

func (s *stubEvaluator) Evaluate(_ context.Context, req automode.Request) (automode.Verdict, error) {
	s.calls++
	s.last = req
	return s.verdict, s.err
}

// permTestDB opens the shared test database; tests skip when no DSN is set.
func permTestDB(t *testing.T) *storage.DB {
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
	return db
}

// permTestUser inserts a throwaway user; everything that cascades from it is
// removed when the test ends.
func permTestUser(t *testing.T, db *storage.DB) string {
	t.Helper()
	ctx := context.Background()
	user := fmt.Sprintf("auto-perm-%d", time.Now().UnixNano())
	// api_key carries a unique constraint; reuse the unique username for it.
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO users (username, api_key, role) VALUES ($1,$1,'user')`, user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM users WHERE username=$1`, user)
	})
	return user
}

// permTestRunner builds a runner backed by the shared test database; tests
// skip when no DATABASE_URL is configured.
func permTestRunner(t *testing.T, eval automode.Evaluator) (*runner, *agentsession.Store) {
	t.Helper()
	db := permTestDB(t)
	user := permTestUser(t, db)
	ctx := context.Background()
	store := &agentsession.Store{DB: db.SQL}
	sess, err := store.Create(ctx, user, "auto perm test", "sysadmin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{
		handler: &Handler{Store: store, AutoMode: eval},
		session: sess,
		ctx:     ctx,
		auto:    true,
		perms:   make(map[string]*permWait),
	}
	return r, store
}

// permTicketRunner builds a runner whose session belongs to a real assistant
// row, so interactive permission requests create a linked assist ticket.
func permTicketRunner(t *testing.T, eval automode.Evaluator) (*runner, *agentsession.Store, *assistticket.Store, string) {
	t.Helper()
	db := permTestDB(t)
	user := permTestUser(t, db)
	ctx := context.Background()
	assistantID := uuid.NewString()
	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO assistants (id, user_id, name, created_at, updated_at)
		VALUES ($1, $2, 'Ada', now(), now())`,
		assistantID, user,
	); err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
	store := &agentsession.Store{DB: db.SQL}
	sess, err := store.Create(ctx, user, "ticket perm test", "sysadmin", "", assistantID)
	if err != nil {
		t.Fatal(err)
	}
	tickets := &assistticket.Store{DB: db.SQL}
	r := &runner{
		handler: &Handler{Store: store, AutoMode: eval, Tickets: tickets},
		session: sess,
		ctx:     ctx,
		perms:   make(map[string]*permWait),
	}
	return r, store, tickets, assistantID
}

func sysagentOptions() []acp.PermissionOption {
	return []acp.PermissionOption{
		{OptionId: "allow", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: "allow_tool", Name: "Allow this tool (session)", Kind: acp.PermissionOptionKindAllowAlways},
		{OptionId: "reject", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
	}
}

func permRequest(title string, opts []acp.PermissionOption, meta map[string]any) acp.RequestPermissionRequest {
	return acp.RequestPermissionRequest{
		SessionId: "s-1",
		Meta:      meta,
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "call-1",
			Title:      &title,
			RawInput:   map[string]any{"command": "rm -rf /"},
		},
		Options: opts,
	}
}

func lastPermissionRow(t *testing.T, store *agentsession.Store, r *runner) *agentsession.Message {
	t.Helper()
	rows, err := store.ListMessages(r.ctx, r.session.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Role == agentsession.RolePermission {
			return rows[i]
		}
	}
	t.Fatal("no permission row persisted")
	return nil
}

func TestRunnerAutoAllowPersistsAndSelectsOrdinaryAllow(t *testing.T) {
	eval := &stubEvaluator{verdict: automode.Verdict{Decision: automode.Allow, Rule: "routine"}}
	r, store := permTestRunner(t, eval)
	if _, err := store.AddMessage(r.ctx, r.session.ID, agentsession.RoleUser, "please clean the build dir", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(r.ctx, r.session.ID, agentsession.RoleUser, "and remove the cache", nil); err != nil {
		t.Fatal(err)
	}

	resp, err := r.onPermission(permRequest("Bash", sysagentOptions(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Selected == nil || string(resp.Outcome.Selected.OptionId) != "allow" {
		t.Fatalf("want allow selected, got %+v", resp.Outcome)
	}
	if eval.calls != 1 {
		t.Fatalf("classifier calls = %d, want 1", eval.calls)
	}
	if !strings.Contains(eval.last.UserDigest, "clean the build dir") ||
		!strings.Contains(eval.last.UserDigest, "remove the cache") {
		t.Fatalf("digest missing conversation: %q", eval.last.UserDigest)
	}
	if strings.Index(eval.last.UserDigest, "clean the build dir") >
		strings.Index(eval.last.UserDigest, "remove the cache") {
		t.Fatalf("digest not chronological: %q", eval.last.UserDigest)
	}
	if !strings.Contains(eval.last.Args, "rm -rf /") {
		t.Fatalf("request args missing: %q", eval.last.Args)
	}
	row := lastPermissionRow(t, store, r)
	var meta agentsession.PermissionMeta
	if err := json.Unmarshal(row.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Outcome != "auto" || meta.OptionID != "allow" {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestRunnerAutoDenySelectsRejectAndRecordsReason(t *testing.T) {
	eval := &stubEvaluator{verdict: automode.Verdict{
		Decision: automode.HardDeny,
		Rule:     "Data Exfiltration",
		Reason:   "sends workspace content somewhere untrusted",
	}}
	r, store := permTestRunner(t, eval)

	resp, err := r.onPermission(permRequest("Bash", sysagentOptions(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Selected == nil || string(resp.Outcome.Selected.OptionId) != "reject" {
		t.Fatalf("want reject selected, got %+v", resp.Outcome)
	}
	row := lastPermissionRow(t, store, r)
	var meta agentsession.PermissionMeta
	if err := json.Unmarshal(row.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Outcome != "auto_deny" || meta.Rule != "Data Exfiltration" ||
		meta.Reason != "sends workspace content somewhere untrusted" {
		t.Fatalf("meta = %+v", meta)
	}
	if !strings.Contains(row.Content, "Data Exfiltration") || !strings.Contains(row.Content, "untrusted") {
		t.Fatalf("row content must show rule and reason: %q", row.Content)
	}
	if len(r.perms) != 0 {
		t.Fatalf("permission wait entries left behind: %+v", r.perms)
	}
}

func TestRunnerAutoDenyWithoutRejectOptionCancels(t *testing.T) {
	eval := &stubEvaluator{verdict: automode.Verdict{Decision: automode.SoftDeny, Rule: "r", Reason: "why"}}
	r, _ := permTestRunner(t, eval)
	opts := []acp.PermissionOption{{OptionId: "allow", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce}}

	resp, err := r.onPermission(permRequest("Bash", opts, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Cancelled == nil {
		t.Fatalf("want cancelled outcome, got %+v", resp.Outcome)
	}
}

func TestRunnerAutoQuestionSkipsClassifier(t *testing.T) {
	eval := &stubEvaluator{verdict: automode.Verdict{Decision: automode.SoftDeny, Rule: "would-block"}}
	r, store := permTestRunner(t, eval)
	opts := []acp.PermissionOption{
		{OptionId: "opt-0", Name: "Approve", Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: "opt-1", Name: "Reject", Kind: acp.PermissionOptionKindAllowOnce},
	}
	meta := map[string]any{"roundpen.kind": "question"}

	resp, err := r.onPermission(permRequest("Approve this plan?", opts, meta))
	if err != nil {
		t.Fatal(err)
	}
	if eval.calls != 0 {
		t.Fatalf("questions must not reach the classifier, calls = %d", eval.calls)
	}
	if resp.Outcome.Selected == nil || string(resp.Outcome.Selected.OptionId) != "opt-0" {
		t.Fatalf("want first option selected, got %+v", resp.Outcome)
	}
	row := lastPermissionRow(t, store, r)
	var rowMeta agentsession.PermissionMeta
	if err := json.Unmarshal(row.Meta, &rowMeta); err != nil {
		t.Fatal(err)
	}
	if rowMeta.Outcome != "auto" {
		t.Fatalf("meta = %+v", rowMeta)
	}
}

func TestRunnerAutoEvaluatorErrorFailsClosed(t *testing.T) {
	eval := &stubEvaluator{
		verdict: automode.Verdict{Decision: automode.SoftDeny, Rule: "classifier_unavailable", Reason: "boom"},
		err:     context.DeadlineExceeded,
	}
	r, store := permTestRunner(t, eval)

	resp, err := r.onPermission(permRequest("Bash", sysagentOptions(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Selected == nil || string(resp.Outcome.Selected.OptionId) != "reject" {
		t.Fatalf("failed classifier must block, got %+v", resp.Outcome)
	}
	row := lastPermissionRow(t, store, r)
	if !strings.Contains(row.Content, "classifier_unavailable") {
		t.Fatalf("denial must record the failure: %q", row.Content)
	}
}

func TestRunnerAutoOffKeepsInteractivePath(t *testing.T) {
	eval := &stubEvaluator{verdict: automode.Verdict{Decision: automode.Allow}}
	r, store := permTestRunner(t, eval)
	r.auto = false

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := r.onPermission(permRequest("Bash", sysagentOptions(), nil)); err != nil {
			t.Errorf("onPermission: %v", err)
		}
	}()
	// No client is connected: the interactive path cancels the pending request.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("interactive path did not cancel without clients")
	}
	if eval.calls != 0 {
		t.Fatalf("auto off must not classify, calls = %d", eval.calls)
	}
	rows, err := store.ListMessages(r.ctx, r.session.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var outcomes []string
	for _, row := range rows {
		if row.Role != agentsession.RolePermission {
			continue
		}
		var meta agentsession.PermissionMeta
		if err := json.Unmarshal(row.Meta, &meta); err != nil {
			t.Fatal(err)
		}
		outcomes = append(outcomes, meta.Outcome)
	}
	if len(outcomes) < 2 || outcomes[len(outcomes)-2] != "requested" || outcomes[len(outcomes)-1] != "cancelled" {
		t.Fatalf("interactive path outcomes = %v, want ... requested, cancelled", outcomes)
	}
}

func TestRunnerNoClientCancelClosesTicket(t *testing.T) {
	eval := &stubEvaluator{verdict: automode.Verdict{Decision: automode.Allow}}
	r, _, tickets, assistantID := permTicketRunner(t, eval)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := r.onPermission(permRequest("Bash", sysagentOptions(), nil)); err != nil {
			t.Errorf("onPermission: %v", err)
		}
	}()
	// No client is connected, so the interactive path abandons the request; the
	// assist ticket must not stay pending (nobody could ever act on it).
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("interactive path did not cancel without clients")
	}

	list, err := tickets.ListByAssistant(r.ctx, r.session.UserID, assistantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("tickets = %d, want 1", len(list))
	}
	if list[0].Status != assistticket.StatusCancelled || list[0].ResolutionNote == "" {
		t.Fatalf("ticket after cancel = %+v, want status cancelled with note", list[0])
	}
	pending, err := tickets.ListPendingByUser(r.ctx, r.session.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending tickets = %d, want 0", len(pending))
	}
}

func TestRunnerAnswerResolvesTicket(t *testing.T) {
	eval := &stubEvaluator{verdict: automode.Verdict{Decision: automode.Allow}}
	r, _, tickets, assistantID := permTicketRunner(t, eval)

	create := func(title string) *assistticket.Ticket {
		t.Helper()
		tk, err := tickets.Create(r.ctx, r.session.UserID, assistticket.CreateInput{
			AssistantID: assistantID,
			SessionID:   r.session.ID,
			Kind:        assistticket.KindPermission,
			Title:       title,
		})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	// The runner is the authority on the decision: answering must close the
	// ticket even when the browser's own resolve request never arrives.
	allowed := create("需要确认：Bash")
	rejected := create("需要确认：Edit")
	r.mu.Lock()
	r.perms["call-allow"] = &permWait{ch: make(chan string, 1), options: sysagentOptions(), ticketID: allowed.ID}
	r.perms["call-reject"] = &permWait{ch: make(chan string, 1), options: sysagentOptions(), ticketID: rejected.ID}
	r.mu.Unlock()

	r.answerPermission("call-allow", "allow")
	r.answerPermission("call-reject", "reject")

	gotAllowed, err := tickets.Get(r.ctx, allowed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotAllowed.Status != assistticket.StatusResolved || gotAllowed.Resolution != assistticket.ResAllowOnce ||
		gotAllowed.ResolutionNote != "allow" {
		t.Fatalf("allowed ticket = %+v", gotAllowed)
	}
	gotRejected, err := tickets.Get(r.ctx, rejected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRejected.Status != assistticket.StatusRejected || gotRejected.Resolution != assistticket.ResReject {
		t.Fatalf("rejected ticket = %+v", gotRejected)
	}
	pending, err := tickets.ListPendingByUser(r.ctx, r.session.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending tickets = %d, want 0", len(pending))
	}
}
