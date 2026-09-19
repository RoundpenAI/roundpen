package issue

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

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

// insertUser creates the user issues.user_id references. Cleanup deletes it,
// which cascades to the test's issues, tasks and docs.
func insertUser(t *testing.T, st *Store, user string) {
	t.Helper()
	ctx := context.Background()
	if _, err := st.DB.ExecContext(ctx, `
		INSERT INTO users (username, email, role, api_key, created_at, updated_at)
		VALUES ($1, $2, 'user', $3, now(), now())
		ON CONFLICT (username) DO NOTHING`,
		user, user+"@example.com", "rp-"+user,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.DB.ExecContext(ctx, `DELETE FROM users WHERE username=$1`, user)
	})
}

func ptr[T any](v T) *T { return &v }

func TestStore_IssueLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	user := "issue-test-user"
	insertUser(t, st, user)

	it, err := st.Create(ctx, user, CreateInput{Title: "议题跟踪系统", Summary: "跟踪器"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(it.Key, "ISS-") {
		t.Fatalf("want ISS- key, got %q", it.Key)
	}
	if it.Status != StatusDrafting {
		t.Fatalf("new issue must start drafting, got %s", it.Status)
	}

	// spec 写入 current → specced
	doc, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocSpec, ContentMD: "# Spec v1", Status: DocCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Status != DocCurrent {
		t.Fatalf("want v1 current, got v%d %s", doc.Version, doc.Status)
	}
	got, _, _, err := st.Get(ctx, user, it.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSpecced {
		t.Fatalf("spec current must advance to specced, got %s", got.Status)
	}

	// spec v2 → v1 superseded，状态保持 specced
	doc2, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocSpec, ContentMD: "# Spec v2", Status: DocCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if doc2.Version != 2 {
		t.Fatalf("want v2, got v%d", doc2.Version)
	}
	docs, err := st.ListDocs(ctx, user, it.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Status != DocCurrent || docs[1].Status != DocSuperseded {
		t.Fatalf("want v2 current + v1 superseded, got %+v", docs)
	}
	for _, d := range docs {
		if d.ContentMD != "" {
			t.Fatalf("list must not carry bodies, got %q", d.ContentMD)
		}
	}

	// plan → planned；任务开工 → in_progress；全部完成 → done
	if _, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocPlan, ContentMD: "# Plan", Status: DocCurrent}); err != nil {
		t.Fatal(err)
	}
	tk, err := st.CreateTask(ctx, user, it.Key, TaskInput{Title: "加表", Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tk.Key, "TSK-") {
		t.Fatalf("want TSK- key, got %q", tk.Key)
	}
	if _, err := st.UpdateTask(ctx, user, tk.Key, TaskUpdateInput{Status: ptr(TaskInProgress)}); err != nil {
		t.Fatal(err)
	}
	got, _, _, _ = st.Get(ctx, user, it.Key)
	if got.Status != StatusInProgress {
		t.Fatalf("task start must advance to in_progress, got %s", got.Status)
	}
	if _, err := st.UpdateTask(ctx, user, tk.Key, TaskUpdateInput{Status: ptr(TaskDone)}); err != nil {
		t.Fatal(err)
	}
	got, _, tasks, _ := st.Get(ctx, user, it.Key)
	if got.Status != StatusDone || got.ClosedAt == nil {
		t.Fatalf("all tasks done must close the issue, got %s", got.Status)
	}
	if tasks[0].DoneAt == nil {
		t.Fatal("done task must carry done_at")
	}

	// 重开任务 → 回到 in_progress
	if _, err := st.UpdateTask(ctx, user, tk.Key, TaskUpdateInput{Status: ptr(TaskTodo)}); err != nil {
		t.Fatal(err)
	}
	got, _, _, _ = st.Get(ctx, user, it.Key)
	if got.Status != StatusInProgress || got.ClosedAt != nil {
		t.Fatalf("reopening a task must reopen the issue, got %s closed=%v", got.Status, got.ClosedAt)
	}
}

func TestStore_ScopedByUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "issue-user-a")
	insertUser(t, st, "issue-user-b")

	it, err := st.Create(ctx, "issue-user-a", CreateInput{Title: "A 的议题"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := st.CreateTask(ctx, "issue-user-a", it.Key, TaskInput{Title: "A 的任务"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.Get(ctx, "issue-user-b", it.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user read must be not found, got %v", err)
	}
	if _, err := st.UpdateTask(ctx, "issue-user-b", tk.Key, TaskUpdateInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user task update must be not found, got %v", err)
	}
	if _, err := st.WriteDoc(ctx, "issue-user-b", it.Key, DocInput{Kind: DocSpec, ContentMD: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user doc write must be not found, got %v", err)
	}
}

func TestStore_Update(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	user := "issue-patch-user"
	insertUser(t, st, user)

	it, err := st.Create(ctx, user, CreateInput{Title: "显式更新"})
	if err != nil {
		t.Fatal(err)
	}
	// the explicit PATCH skips the status machine: drafting -> done is the
	// user's call, and closed_at follows the status
	got, err := st.Update(ctx, user, it.Key, UpdateInput{Status: ptr(StatusDone)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDone || got.ClosedAt == nil {
		t.Fatalf("explicit done must close the issue, got %s closed=%v", got.Status, got.ClosedAt)
	}
	got, err = st.Update(ctx, user, it.Key, UpdateInput{Status: ptr(StatusInProgress)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusInProgress || got.ClosedAt != nil {
		t.Fatalf("leaving done must clear closed_at, got %s closed=%v", got.Status, got.ClosedAt)
	}
	// a partial update leaves the other fields alone
	got, err = st.Update(ctx, user, it.Key, UpdateInput{Title: ptr("改名")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "改名" || got.Status != StatusInProgress || it.Summary != got.Summary {
		t.Fatalf("partial update: %+v", got)
	}
	if _, err := st.Update(ctx, user, it.Key, UpdateInput{Status: ptr("weird")}); err == nil {
		t.Fatal("invalid status must fail validation")
	}
	if _, err := st.Update(ctx, "issue-user-b", it.Key, UpdateInput{Title: ptr("改名")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user update must be not found, got %v", err)
	}
}

func TestStore_GetDoc(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	user := "issue-doc-user"
	insertUser(t, st, user)

	it, err := st.Create(ctx, user, CreateInput{Title: "文档读取"})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocSpec, ContentMD: "# v1", Status: DocDraft})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocSpec, ContentMD: "# v2", Status: DocCurrent})
	if err != nil {
		t.Fatal(err)
	}

	// latest / empty prefer the current row, body included
	for _, docKey := range []string{"latest", ""} {
		got, err := st.GetDoc(ctx, user, it.Key, docKey, 0)
		if err != nil {
			t.Fatalf("get %q: %v", docKey, err)
		}
		if got.ID != v2.ID || got.ContentMD != "# v2" {
			t.Fatalf("get %q: want current v2, got v%d %q", docKey, got.Version, got.ContentMD)
		}
	}
	// a short key or uuid reads that exact version
	for _, docKey := range []string{v1.Key, v1.ID} {
		got, err := st.GetDoc(ctx, user, it.Key, docKey, 0)
		if err != nil {
			t.Fatalf("get %q: %v", docKey, err)
		}
		if got.ID != v1.ID || got.ContentMD != "# v1" {
			t.Fatalf("get %q: want v1, got v%d %q", docKey, got.Version, got.ContentMD)
		}
	}
	// an explicit version pins the row
	got, err := st.GetDoc(ctx, user, it.Key, "latest", v1.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != v1.ID {
		t.Fatalf("want v1 by version, got v%d", got.Version)
	}

	// no current row → the highest version
	drafts, err := st.Create(ctx, user, CreateInput{Title: "只有草稿"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteDoc(ctx, user, drafts.Key, DocInput{Kind: DocPlan, ContentMD: "# d1", Status: DocDraft}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteDoc(ctx, user, drafts.Key, DocInput{Kind: DocPlan, ContentMD: "# d2", Status: DocDraft}); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetDoc(ctx, user, drafts.Key, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.ContentMD != "# d2" {
		t.Fatalf("want highest draft v2, got v%d %q", got.Version, got.ContentMD)
	}
	if it2, _, _, err := st.Get(ctx, user, drafts.Key); err != nil || it2.Status != StatusDrafting {
		t.Fatalf("drafts must not advance the issue, got %v %v", it2, err)
	}

	if _, err := st.GetDoc(ctx, "issue-user-b", it.Key, "latest", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user doc read must be not found, got %v", err)
	}
}

// Ticking a task straight from todo is the console's checkbox path. It must
// close the issue even though the task never passed through in_progress.
func TestStore_TickingLastTaskClosesIssue(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	user := "issue-test-user"
	insertUser(t, st, user)

	it, err := st.Create(ctx, user, CreateInput{Title: "复选框路径"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocSpec, ContentMD: "# Spec", Status: DocCurrent}); err != nil {
		t.Fatal(err)
	}
	tk, err := st.CreateTask(ctx, user, it.Key, TaskInput{Title: "一步"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _, err := st.Get(ctx, user, it.Key); err != nil || got.Status != StatusSpecced {
		t.Fatalf("want specced before the task is ticked, got %v %v", got, err)
	}

	if _, err := st.UpdateTask(ctx, user, tk.Key, TaskUpdateInput{Status: ptr(TaskDone)}); err != nil {
		t.Fatal(err)
	}
	got, _, tasks, err := st.Get(ctx, user, it.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDone || got.ClosedAt == nil {
		t.Fatalf("last task ticked must close the issue, got %s closed=%v", got.Status, got.ClosedAt)
	}
	if tasks[0].DoneAt == nil {
		t.Fatal("done task must carry done_at")
	}
}
