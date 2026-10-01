package routine

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set ROUNDPEN_TEST_DATABASE_URL")
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
	return &Store{DB: db.SQL}
}

func insertUser(t *testing.T, st *Store, user string) {
	t.Helper()
	ctx := context.Background()
	if _, err := st.DB.ExecContext(ctx, `
		INSERT INTO users (username, email, role, api_key, created_at, updated_at)
		VALUES ($1, $2, 'user', $3, now(), now())
		ON CONFLICT (username) DO NOTHING`,
		user, user+"@example.com", "rp-routine-"+user,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.DB.ExecContext(ctx, `DELETE FROM users WHERE username=$1`, user)
	})
}

func insertAssistant(t *testing.T, st *Store, id, user string) {
	t.Helper()
	if _, err := st.DB.ExecContext(context.Background(), `
		INSERT INTO assistants (id, user_id, name, capabilities, network_tier)
		VALUES ($1, $2, 'routine-test', '{"shell":true,"browser":true,"mobile":false,"desktop":false}', 'all')
		ON CONFLICT (id) DO NOTHING`, id, user); err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
}

func TestStore_CreateListAndIsolate(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-a")
	insertUser(t, st, "routine-b")
	insertAssistant(t, st, "asst-a", "routine-a")

	rt, err := st.Create(ctx, "routine-a", CreateInput{
		AssigneeAssistantID: "asst-a",
		Title:               "论文周报",
		Brief:               "每周汇总",
		Autonomy:            AutonomyRead,
		Cron:                "0 8 * * 1",
		Timezone:            "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rt.Key, "RTN-") {
		t.Fatalf("key %q", rt.Key)
	}
	if rt.NextRunAt == nil || !rt.NextRunAt.After(time.Now().Add(-time.Minute)) {
		t.Fatalf("next_run_at = %v", rt.NextRunAt)
	}

	other, err := st.List(ctx, "routine-b", ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("user b saw %d routines", len(other))
	}
}

func TestStore_ClaimFireOverlapAndStale(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-claim")
	insertAssistant(t, st, "asst-claim", "routine-claim")
	rt, err := st.Create(ctx, "routine-claim", CreateInput{
		AssigneeAssistantID: "asst-claim",
		Title:               "周报",
		Autonomy:            AutonomyRead,
		Cron:                "0 8 * * 1",
		Timezone:            "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	due := time.Now().Add(-time.Hour)
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, due); err != nil {
		t.Fatal(err)
	}
	claims, err := st.ClaimDue(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].Run.Status != RunQueued {
		t.Fatalf("claims = %+v", claims)
	}
	if claims[0].Routine.NextRunAt == nil || !claims[0].Routine.NextRunAt.After(time.Now()) {
		t.Fatalf("next not in the future: %v", claims[0].Routine.NextRunAt)
	}

	// The open run blocks the next slot. Pull next_run_at back so the row is due again.
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	again, err := st.ClaimDue(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("overlap must not start a run, got %+v", again)
	}
	runs, err := st.ListRuns(ctx, "routine-claim", rt.Key, 10)
	if err != nil {
		t.Fatal(err)
	}
	var overlaps int
	for _, rn := range runs {
		if rn.Status == RunSkippedOverlap {
			overlaps++
		}
	}
	if overlaps != 1 {
		t.Fatalf("want one skipped_overlap, runs=%+v", runs)
	}

	// A fresh routine missed by more than the weekly grace records skipped_stale.
	stale, err := st.Create(ctx, "routine-claim", CreateInput{
		AssigneeAssistantID: "asst-claim",
		Title:               "过期",
		Autonomy:            AutonomyRead,
		Cron:                "0 8 * * 1",
		Timezone:            "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, stale.ID, time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := st.ClaimDue(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Routine.ID == stale.ID {
			t.Fatalf("stale routine was started: %+v", c)
		}
	}
	staleRuns, err := st.ListRuns(ctx, "routine-claim", stale.Key, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(staleRuns) != 1 || staleRuns[0].Status != RunSkippedStale {
		t.Fatalf("stale runs = %+v", staleRuns)
	}
}

func TestStore_StateLimitAndFinishSession(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-state")
	insertAssistant(t, st, "asst-state", "routine-state")
	rt, err := st.Create(ctx, "routine-state", CreateInput{
		AssigneeAssistantID: "asst-state",
		Title:               "状态",
		Autonomy:            AutonomyRead,
		Cron:                "0 8 * * 1",
		Timezone:            "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	big := json.RawMessage(`{"x":"` + strings.Repeat("a", MaxStateBytes) + `"}`)
	if _, err := st.UpdateState(ctx, "routine-state", rt.Key, big, "asst-state"); err == nil {
		t.Fatal("oversized state must fail")
	}
	if _, err := st.UpdateState(ctx, "routine-state", rt.Key, json.RawMessage(`{"seen":["a"]}`), "other"); err != ErrForbidden {
		t.Fatalf("wrong assignee: %v", err)
	}

	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	claims, err := st.ClaimDue(ctx, 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim: %v %+v", err, claims)
	}
	if err := st.AttachSession(ctx, claims[0].Run.ID, "sess-real"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.MarkRunning(ctx, claims[0].Run.ID); err != nil || !ok {
		t.Fatalf("mark running: ok=%v err=%v", ok, err)
	}
	if _, err := st.FinishRun(ctx, "routine-state", claims[0].Run.Key, "sess-other", "nope", nil); err != ErrForbidden {
		t.Fatalf("wrong session: %v", err)
	}
	done, err := st.FinishRun(ctx, "routine-state", claims[0].Run.Key, "sess-real", "写好了", nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != RunSucceeded || done.Summary != "写好了" {
		t.Fatalf("finish = %+v", done)
	}
}
