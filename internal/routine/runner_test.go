package routine

import (
	"context"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type readyEnv struct{}

func (readyEnv) EnsureAgent(context.Context, string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{ID: "sb"}, nil
}
func (readyEnv) EnsureBrowser(context.Context, string) (*userenv.BrowserTarget, error) {
	return &userenv.BrowserTarget{}, nil
}

type oneUser struct{ u *storage.User }

func (o oneUser) GetByUsername(context.Context, string) (*storage.User, error) { return o.u, nil }

type fakeExec struct {
	store    *Store
	user     string
	autonomy string
	starts   int
	cancels  int
	runKey   string
	block    bool
}

func (f *fakeExec) StartRoutine(_ context.Context, _ *storage.User, _, _, runKey, autonomy string) (string, error) {
	f.starts++
	f.autonomy = autonomy
	f.runKey = runKey
	return "sess-" + runKey, nil
}
func (f *fakeExec) ResumeRoutine(context.Context, *storage.User, string, string, string) error {
	return nil
}
func (f *fakeExec) Prompt(ctx context.Context, sessionID, _ string) (acp.StopReason, error) {
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	_, err := f.store.FinishRun(ctx, f.user, f.runKey, sessionID, "写好了", nil)
	return "", err
}
func (f *fakeExec) Cancel(context.Context, string) error {
	f.cancels++
	return nil
}

func waitRun(t *testing.T, st *Store, user, key, status string) *Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := st.ListRuns(context.Background(), user, key, 5)
		if err == nil {
			for i := range runs {
				if runs[i].Status == status {
					return &runs[i]
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach %s", key, status)
	return nil
}

func TestRunner_ExecutesAndSummarizes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-run")
	insertAssistant(t, st, "asst-run", "routine-run")
	sessions := &agentsession.Store{DB: st.DB}
	if _, err := sessions.CreateKind(ctx, "routine-run", "主对话", "sysadmin", "", "asst-run", "chat"); err != nil {
		t.Fatal(err)
	}
	rt, err := st.Create(ctx, "routine-run", CreateInput{
		AssigneeAssistantID: "asst-run", Title: "周报", Autonomy: AutonomyRead,
		Cron: "0 8 * * 1", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{store: st, user: "routine-run"}
	r := &Runner{
		Store: st, Users: oneUser{u: &storage.User{Username: "routine-run"}},
		Assistants: &assistant.Store{DB: st.DB}, Envs: readyEnv{},
		Starter: exec, ACP: exec, Sessions: sessions,
	}
	r.TickOnce(ctx)
	got := waitRun(t, st, "routine-run", rt.Key, RunSucceeded)
	if exec.starts != 1 || exec.autonomy != AutonomyRead {
		t.Fatalf("starts=%d autonomy=%s", exec.starts, exec.autonomy)
	}
	if got.Summary != "写好了" {
		t.Fatalf("summary %q", got.Summary)
	}
	list, err := sessions.ListByAssistant(ctx, "routine-run", "asst-run", 5)
	if err != nil || len(list) != 1 {
		t.Fatalf("primary sessions: %v %+v", err, list)
	}
	msgs, err := sessions.ListMessages(ctx, list[0].ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "写好了") || !strings.Contains(msgs[0].Content, rt.Key) {
		t.Fatalf("primary summary = %+v", msgs)
	}
}

func TestRunner_TimeoutCancels(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-timeout")
	insertAssistant(t, st, "asst-timeout", "routine-timeout")
	rt, err := st.Create(ctx, "routine-timeout", CreateInput{
		AssigneeAssistantID: "asst-timeout", Title: "慢", Autonomy: AutonomyRead,
		Cron: "0 8 * * 1", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{store: st, user: "routine-timeout", block: true}
	r := &Runner{
		Store: st, Users: oneUser{u: &storage.User{Username: "routine-timeout"}},
		Assistants: &assistant.Store{DB: st.DB}, Envs: readyEnv{},
		Starter: exec, ACP: exec, BudgetLimit: 40 * time.Millisecond,
	}
	r.TickOnce(ctx)
	waitRun(t, st, "routine-timeout", rt.Key, RunFailed)
	if exec.cancels == 0 {
		t.Fatal("timeout did not cancel")
	}
}

func TestRunner_PausesWhenNetworkClosed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-nonet")
	if _, err := st.DB.ExecContext(ctx, `
		INSERT INTO assistants (id, user_id, name, network_tier)
		VALUES ('asst-nonet', 'routine-nonet', 'closed', 'none')`); err != nil {
		t.Fatal(err)
	}
	rt, err := st.Create(ctx, "routine-nonet", CreateInput{
		AssigneeAssistantID: "asst-nonet", Title: "没网", Autonomy: AutonomyRead,
		Cron: "0 8 * * 1", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{store: st, user: "routine-nonet"}
	r := &Runner{
		Store: st, Users: oneUser{u: &storage.User{Username: "routine-nonet"}},
		Assistants: &assistant.Store{DB: st.DB}, Envs: readyEnv{},
		Starter: exec, ACP: exec,
	}
	r.TickOnce(ctx)
	waitRun(t, st, "routine-nonet", rt.Key, RunFailed)
	if exec.starts != 0 {
		t.Fatal("closed network still started a session")
	}
	got, _, err := st.Get(ctx, "routine-nonet", rt.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPaused || got.NextRunAt != nil {
		t.Fatalf("status=%s next=%v", got.Status, got.NextRunAt)
	}
}

func TestRunner_RejectFailsAndSummarizes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-reject")
	insertAssistant(t, st, "asst-reject", "routine-reject")
	sessions := &agentsession.Store{DB: st.DB}
	if _, err := sessions.CreateKind(ctx, "routine-reject", "主对话", "sysadmin", "", "asst-reject", "chat"); err != nil {
		t.Fatal(err)
	}
	rt, err := st.Create(ctx, "routine-reject", CreateInput{
		AssigneeAssistantID: "asst-reject", Title: "付款", Autonomy: AutonomyRead,
		Cron: "0 8 * * 1", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	claims, err := st.ClaimDue(ctx, 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim: %v %+v", err, claims)
	}
	rn, ok, err := st.MarkRunning(ctx, claims[0].Run.ID)
	if err != nil || !ok {
		t.Fatalf("mark: ok=%v err=%v", ok, err)
	}
	if err := st.AttachSession(ctx, rn.ID, "sess-reject"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RequestConfirm(ctx, "routine-reject", rn.Key, "sess-reject", "ticket-reject", 120); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Store: st, Sessions: sessions}
	r.HandleTicket(ctx, &assistticket.Ticket{
		ID: "ticket-reject", Resolution: assistticket.ResReject, ResolutionNote: "先别付",
	})
	got := waitRun(t, st, "routine-reject", rt.Key, RunFailed)
	if !strings.Contains(got.Summary, "先别付") {
		t.Fatalf("summary %q", got.Summary)
	}
	list, err := sessions.ListByAssistant(ctx, "routine-reject", "asst-reject", 1)
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	msgs, err := sessions.ListMessages(ctx, list[0].ID, 5)
	if err != nil || len(msgs) != 1 || !strings.Contains(msgs[0].Content, "先别付") {
		t.Fatalf("primary summary = %+v err=%v", msgs, err)
	}
}
