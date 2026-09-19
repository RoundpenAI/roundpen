package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/commands"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

const gsyncSkillMD = `---
name: gsync
description: Sync generated code across the workspace.
args: true
---
Run gsync from the repository root and fix any drift it reports.
`

// catalogEnvStore is an in-memory slot store for catalog tests.
type catalogEnvStore struct{ mappings map[string]userenv.Mapping }

func (s catalogEnvStore) Get(_ context.Context, userID, slot string) (*userenv.Mapping, error) {
	if m, ok := s.mappings[slot]; ok {
		m.UserID = userID
		return &m, nil
	}
	return nil, nil
}

func (s catalogEnvStore) List(_ context.Context, userID string) ([]userenv.Mapping, error) {
	out := make([]userenv.Mapping, 0, len(s.mappings))
	for _, m := range s.mappings {
		m.UserID = userID
		out = append(out, m)
	}
	return out, nil
}

func (s catalogEnvStore) Upsert(context.Context, string, string, string, string) error { return nil }
func (s catalogEnvStore) Delete(context.Context, string, string) error                 { return nil }

// catalogSandboxes only fakes what the catalog path touches. Anything else
// (Resolve/Create/Connect) hits the nil embedded interface and panics loudly —
// which is the assertion that listing never provisions a container.
type catalogSandboxes struct {
	sandbox.Manager
	status sandbox.Status
}

func (f *catalogSandboxes) Get(context.Context, string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{ID: "sb-agent", Status: f.status}, nil
}

func (f *catalogSandboxes) Resolve(context.Context, sandbox.ResolveRequest) (*sandbox.Sandbox, error) {
	return nil, nil
}

func (f *catalogSandboxes) Exec(_ context.Context, _ string, req sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	mode := ""
	if len(req.Cmd) > 3 {
		mode = req.Cmd[3]
	}
	switch mode {
	case "skill-list":
		return &sandbox.ExecResult{Stdout: []byte("gsync\n")}, nil
	case "skill-read":
		return &sandbox.ExecResult{Stdout: []byte(gsyncSkillMD)}, nil
	}
	return &sandbox.ExecResult{}, nil
}

func catalogHandler(status sandbox.Status) *Handler {
	boxes := &catalogSandboxes{status: status}
	return &Handler{
		Envs: &userenv.Service{
			Store: catalogEnvStore{mappings: map[string]userenv.Mapping{
				userenv.SlotAgent: {Slot: userenv.SlotAgent, SandboxID: "sb-agent"},
			}},
			Sandboxes: boxes,
		},
		Sandboxes: boxes,
	}
}

func catalogNames(cat []commands.Command) []string {
	out := make([]string, 0, len(cat))
	for _, c := range cat {
		out = append(out, c.Name)
	}
	return out
}

func TestCommandCatalogBuiltinsWithoutEnvironment(t *testing.T) {
	h := &Handler{}
	cat := h.commandCatalog(context.Background(), &agentsession.Session{ID: "s", UserID: "alice"})
	got := catalogNames(cat)
	want := []string{"clear", "help", "commit", "fix", "review", "summarize"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("catalog=%v want %v", got, want)
	}
	for _, c := range cat {
		if c.Kind == commands.KindAction && c.Source != commands.SourceAction {
			t.Fatalf("%s source=%q", c.Name, c.Source)
		}
		if c.Kind == commands.KindSkill && c.Source != commands.SourceBuiltin {
			t.Fatalf("%s source=%q", c.Name, c.Source)
		}
		switch c.Name {
		case "review":
			if _, ok := commands.SkillDisplayDescription("review"); !ok || c.Description != "审查最近改动，给出 bug / 安全 / 可读性结论" {
				t.Fatalf("review description=%q", c.Description)
			}
			if c.Args {
				t.Fatal("review does not take args")
			}
		case "commit":
			if !c.Args {
				t.Fatal("commit takes args")
			}
		}
	}
}

func TestCommandCatalogListsInstalledSkillsOnlyWhenAgentRunning(t *testing.T) {
	sess := &agentsession.Session{ID: "s", UserID: "alice"}

	running := catalogHandler(sandbox.StatusRunning).
		commandCatalog(context.Background(), sess)
	var gsync *commands.Command
	for i := range running {
		if running[i].Name == "gsync" {
			gsync = &running[i]
		}
	}
	if gsync == nil {
		t.Fatalf("installed skill missing from catalog: %v", catalogNames(running))
	}
	if gsync.Source != commands.SourceInstalled || gsync.Kind != commands.KindSkill || !gsync.Args {
		t.Fatalf("gsync=%+v", *gsync)
	}
	if gsync.Description != "Sync generated code across the workspace." {
		t.Fatalf("gsync description=%q", gsync.Description)
	}

	stopped := catalogHandler(sandbox.StatusStopped).
		commandCatalog(context.Background(), sess)
	for _, c := range stopped {
		if c.Name == "gsync" {
			t.Fatal("installed skills must not be listed while the agent environment is stopped")
		}
	}
}

func commandTestStore(t *testing.T) (*agentsession.Store, *storage.DB) {
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
	return &agentsession.Store{DB: db.SQL}, db
}

// commandTestRunner builds a runner backed by the shared test database.
func commandTestRunner(t *testing.T) (*runner, *agentsession.Store) {
	t.Helper()
	ctx := context.Background()
	store, db := commandTestStore(t)
	user := fmt.Sprintf("cmd-test-%d", time.Now().UnixNano())
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO users (username, api_key, role) VALUES ($1,$1,'user')`, user); err != nil {
		t.Fatal(err)
	}
	sess, err := store.Create(ctx, user, "slash command test", "sysadmin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mgr := manager.New(nil, nil, providers.Default(), manager.SysDeps{})
	r := &runner{
		handler: &Handler{Store: store, ACP: mgr},
		session: sess,
		acp:     mgr,
		actor:   manager.Actor{Username: user, Role: "user"},
		ctx:     ctx,
		cancel:  func() {},
		clients: map[*wsClient]struct{}{},
		auto:    true,
		perms:   map[string]*permWait{},
	}
	return r, store
}

func (r *runner) testRows(t *testing.T) []*agentsession.Message {
	t.Helper()
	rows, err := r.handler.Store.ListMessages(r.ctx, r.session.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestListCommandsRoute(t *testing.T) {
	store, db := commandTestStore(t)
	h := &Handler{Store: store}
	if _, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO users (username, api_key, role) VALUES ('cmd-route-owner','cmd-route-owner','user')`); err != nil {
		t.Fatal(err)
	}
	sess, err := store.Create(context.Background(), "cmd-route-owner", "route test", "sysadmin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)

	call := func(username string, role storage.UserRole) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/agent-sessions/"+sess.ID+"/commands", nil)
		if username != "" {
			req = req.WithContext(auth.WithUser(req.Context(), &storage.User{Username: username, Role: role}))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if code := call("", storage.RoleUser).Code; code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", code)
	}
	if code := call("someone-else", storage.RoleUser).Code; code != http.StatusForbidden {
		t.Fatalf("foreign user status=%d", code)
	}
	rec := call("cmd-route-owner", storage.RoleUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Commands []commands.Command `json:"commands"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Commands) != 6 {
		t.Fatalf("commands=%v", catalogNames(body.Commands))
	}
}

func TestRunnerSkillCommandPersistsExpansion(t *testing.T) {
	r, _ := commandTestRunner(t)
	r.mu.Lock()
	r.busy = true // queue instead of running a turn
	r.mu.Unlock()

	r.command("/review", "  关注并发  ")

	rows := r.testRows(t)
	if len(rows) != 1 || rows[0].Role != agentsession.RoleUser {
		t.Fatalf("rows=%+v", rows)
	}
	if !strings.Contains(rows[0].Content, `Running skill "review" (builtin)`) ||
		!strings.Contains(rows[0].Content, "git diff HEAD") {
		t.Fatalf("persisted content is not the expanded skill: %q", rows[0].Content)
	}
	var meta map[string]any
	if err := json.Unmarshal(rows[0].Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["type"] != "user" || meta["command"] != "review" ||
		meta["commandArgs"] != "关注并发" || meta["display"] != "/review 关注并发" {
		t.Fatalf("meta=%v", meta)
	}
	r.mu.Lock()
	queued := len(r.pending)
	r.mu.Unlock()
	if queued != 1 {
		t.Fatalf("queued=%d", queued)
	}
}

func TestRunnerUnknownSkillCommandIsRejected(t *testing.T) {
	r, _ := commandTestRunner(t)
	r.command("nope", "")
	if rows := r.testRows(t); len(rows) != 0 {
		t.Fatalf("unknown command must not persist a turn: %+v", rows)
	}
}

func TestRunnerHelpPersistsCatalog(t *testing.T) {
	r, _ := commandTestRunner(t)
	r.command("help", "")
	rows := r.testRows(t)
	if len(rows) != 1 || rows[0].Role != agentsession.RoleEvent {
		t.Fatalf("rows=%+v", rows)
	}
	for _, want := range []string{"/clear", "/review"} {
		if !strings.Contains(rows[0].Content, want) {
			t.Fatalf("help text missing %q: %q", want, rows[0].Content)
		}
	}
	var meta map[string]string
	if err := json.Unmarshal(rows[0].Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["type"] != "event" || meta["command"] != "help" {
		t.Fatalf("meta=%v", meta)
	}
}

func TestRunnerClearAppendsMarkerAndRestartsRuntime(t *testing.T) {
	r, _ := commandTestRunner(t)
	r.command("clear", "")

	waitFor(t, "clear to finish", func() bool {
		r.mu.Lock()
		busy := r.busy
		r.mu.Unlock()
		if busy {
			return false
		}
		_, ok := r.acp.Get(r.session.ID)
		return ok && len(r.testRowsNoFatal()) == 1
	})

	r.mu.Lock()
	oldRT := r.rt
	r.mu.Unlock()
	if oldRT == nil {
		t.Fatal("runner runtime was not replaced")
	}

	rows := r.testRows(t)
	if rows[0].Role != agentsession.RoleEvent || rows[0].Content != "上下文已清空" {
		t.Fatalf("marker row=%+v", rows[0])
	}
	var meta map[string]string
	if err := json.Unmarshal(rows[0].Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["type"] != agentsession.MetaTypeClear {
		t.Fatalf("meta=%v", meta)
	}
}

func TestRunnerClearDefersUntilTurnEnds(t *testing.T) {
	r, _ := commandTestRunner(t)
	r.mu.Lock()
	r.busy = true
	r.pending = []string{"queued while clearing"}
	r.mu.Unlock()

	r.command("clear", "")
	if rows := r.testRows(t); len(rows) != 0 {
		t.Fatalf("marker must wait for the turn to end: %+v", rows)
	}
	r.mu.Lock()
	deferred := r.clearPending
	queued := len(r.pending)
	r.mu.Unlock()
	if !deferred || queued != 0 {
		t.Fatalf("clearPending=%v pending=%d", deferred, queued)
	}

	r.finishTurn()
	waitFor(t, "deferred clear", func() bool {
		_, ok := r.acp.Get(r.session.ID)
		return ok && len(r.testRowsNoFatal()) == 1
	})
}

func (r *runner) testRowsNoFatal() []*agentsession.Message {
	rows, err := r.handler.Store.ListMessages(r.ctx, r.session.ID, 200)
	if err != nil {
		return nil
	}
	return rows
}
