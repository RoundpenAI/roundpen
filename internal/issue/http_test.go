package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// newTestMux mounts the handler the way cmd/roundpend does. A nil store is
// allowed: routes that reject the request before touching the store must not
// dereference it.
func newTestMux(st *Store) *http.ServeMux {
	mux := http.NewServeMux()
	(&Handler{Store: st}).Mount(mux)
	return mux
}

// request builds a request with the caller injected exactly as auth.Middleware
// does; user == "" leaves the request anonymous.
func request(method, path, body, user string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if user != "" {
		r = r.WithContext(auth.WithUser(r.Context(), &storage.User{Username: user}))
	}
	return r
}

func serve(mux *http.ServeMux, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

// singleEnvelope asserts the body holds exactly one non-null top-level key and
// returns its raw value, enforcing the response envelope the web client reads.
func singleEnvelope(t *testing.T, rec *httptest.ResponseRecorder, key string) json.RawMessage {
	t.Helper()
	var env map[string]json.RawMessage
	decodeInto(t, rec, &env)
	if len(env) != 1 {
		t.Fatalf("envelope must hold exactly %q, got %s", key, rec.Body.String())
	}
	raw, ok := env[key]
	if !ok || string(raw) == "null" {
		t.Fatalf("envelope missing non-null %q: %s", key, rec.Body.String())
	}
	return raw
}

func insertAssistant(t *testing.T, st *Store, id, user string) {
	t.Helper()
	if _, err := st.DB.ExecContext(context.Background(), `
		INSERT INTO assistants (id, user_id, name) VALUES ($1, $2, 'issue-http')
		ON CONFLICT (id) DO NOTHING`, id, user); err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
}

// insertSession gives user a session to attribute writes to. assistantID may be
// empty: sessions created before assistants have NULL assistant_id. Cleanup
// rides on insertUser's DELETE (agent_sessions cascades).
func insertSession(t *testing.T, st *Store, id, user, assistantID string) {
	t.Helper()
	if _, err := st.DB.ExecContext(context.Background(), `
		INSERT INTO agent_sessions (id, user_id, title, provider_id, sandbox_id, status, assistant_id)
		VALUES ($1, $2, 'issue-http', 'mock', '', 'active', NULLIF($3, ''))
		ON CONFLICT (id) DO NOTHING`, id, user, assistantID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func TestHandler_RequiresAuth(t *testing.T) {
	mux := newTestMux(nil)
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/v1/issues", ""},
		{http.MethodPost, "/v1/issues", `{"title":"x"}`},
		{http.MethodGet, "/v1/issues/ISS-1", ""},
		{http.MethodPatch, "/v1/issues/ISS-1", `{"status":"done"}`},
		{http.MethodGet, "/v1/issues/ISS-1/docs", ""},
		{http.MethodPost, "/v1/issues/ISS-1/docs", `{"kind":"spec","contentMd":"x"}`},
		{http.MethodGet, "/v1/issues/ISS-1/docs/latest", ""},
		{http.MethodGet, "/v1/issues/ISS-1/tasks", ""},
		{http.MethodPost, "/v1/issues/ISS-1/tasks", `{"title":"t"}`},
		{http.MethodPatch, "/v1/tasks/TSK-1", `{"status":"done"}`},
	}
	for _, rt := range routes {
		rec := serve(mux, request(rt.method, rt.path, rt.body, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: want 401, got %d (%s)", rt.method, rt.path, rec.Code, rec.Body.String())
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
			t.Errorf("%s %s: want {\"error\":...} body, got %s", rt.method, rt.path, rec.Body.String())
		}
	}
}

// TestHandler_RejectsBadInputBeforeStore pins the validation boundary: every
// client mistake is a 400 raised in the handler, before the (nil, therefore
// fatal) store is reached.
func TestHandler_RejectsBadInputBeforeStore(t *testing.T) {
	mux := newTestMux(nil)
	cases := []struct{ name, method, path, body string }{
		{"blank title", http.MethodPost, "/v1/issues", `{"title":"   "}`},
		{"missing title", http.MethodPost, "/v1/issues", `{}`},
		{"invalid json", http.MethodPost, "/v1/issues", `{"title":`},
		{"bad issue status", http.MethodPatch, "/v1/issues/ISS-1", `{"status":"weird"}`},
		{"blank issue title", http.MethodPatch, "/v1/issues/ISS-1", `{"title":" "}`},
		{"bad list status", http.MethodGet, "/v1/issues?status=weird", ""},
		{"bad limit", http.MethodGet, "/v1/issues?limit=abc", ""},
		{"bad doc kind", http.MethodPost, "/v1/issues/ISS-1/docs", `{"kind":"notes","contentMd":"x"}`},
		{"doc missing body", http.MethodPost, "/v1/issues/ISS-1/docs", `{"kind":"spec","contentMd":"  "}`},
		{"bad doc status", http.MethodPost, "/v1/issues/ISS-1/docs", `{"kind":"spec","contentMd":"x","status":"live"}`},
		{"bad doc author", http.MethodPost, "/v1/issues/ISS-1/docs", `{"kind":"spec","contentMd":"x","authorType":"robot"}`},
		{"bad doc list kind", http.MethodGet, "/v1/issues/ISS-1/docs?kind=notes", ""},
		{"blank task title", http.MethodPost, "/v1/issues/ISS-1/tasks", `{"title":" "}`},
		{"bad task status", http.MethodPost, "/v1/issues/ISS-1/tasks", `{"title":"t","status":"weird"}`},
		{"bad patch task status", http.MethodPatch, "/v1/tasks/TSK-1", `{"status":"weird"}`},
		{"blank patch task title", http.MethodPatch, "/v1/tasks/TSK-1", `{"title":""}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serve(mux, request(c.method, c.path, c.body, "issue-http-user"))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandler_StoreUnavailable(t *testing.T) {
	mux := newTestMux(nil)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/issues", ""},
		{http.MethodPost, "/v1/issues", `{"title":"ok"}`},
		{http.MethodGet, "/v1/issues/ISS-1", ""},
		{http.MethodGet, "/v1/issues/ISS-1/docs", ""},
		{http.MethodGet, "/v1/issues/ISS-1/docs/latest", ""},
		{http.MethodGet, "/v1/issues/ISS-1/tasks", ""},
	} {
		rec := serve(mux, request(c.method, c.path, c.body, "issue-http-user"))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: want 503, got %d (%s)", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

func TestHandler_NotFoundAndScoping(t *testing.T) {
	st := newTestStore(t)
	mux := newTestMux(st)
	insertUser(t, st, "issue-http-a")
	insertUser(t, st, "issue-http-b")

	// keys that resolve to nothing answer 404 on every key route
	missing := []struct{ method, path, body string }{
		{http.MethodGet, "/v1/issues/ISS-990001", ""},
		{http.MethodPatch, "/v1/issues/ISS-990001", `{"status":"done"}`},
		{http.MethodPost, "/v1/issues/ISS-990001/docs", `{"kind":"spec","contentMd":"x"}`},
		{http.MethodGet, "/v1/issues/ISS-990001/docs/latest", ""},
		{http.MethodGet, "/v1/issues/ISS-990001/tasks", ""},
		{http.MethodPost, "/v1/issues/ISS-990001/tasks", `{"title":"t"}`},
		{http.MethodPatch, "/v1/tasks/TSK-990001", `{"status":"done"}`},
	}
	for _, c := range missing {
		rec := serve(mux, request(c.method, c.path, c.body, "issue-http-a"))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: want 404, got %d (%s)", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	// B's rows read as 404 through every route when A asks, keyed by short key
	// or uuid — the store's user scoping is the handler's isolation boundary.
	ctx := context.Background()
	it, err := st.Create(ctx, "issue-http-b", CreateInput{Title: "B 的议题", Origin: OriginConsole})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := st.WriteDoc(ctx, "issue-http-b", it.Key, DocInput{Kind: DocSpec, ContentMD: "# B"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(ctx, "issue-http-b", it.Key, TaskInput{Title: "B 的任务"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := []struct{ method, path, body string }{
		{http.MethodGet, "/v1/issues/" + it.Key, ""},
		{http.MethodGet, "/v1/issues/" + it.ID, ""},
		{http.MethodPatch, "/v1/issues/" + it.Key, `{"title":"hijack"}`},
		{http.MethodPost, "/v1/issues/" + it.Key + "/docs", `{"kind":"spec","contentMd":"x"}`},
		{http.MethodGet, "/v1/issues/" + it.Key + "/docs/" + doc.Key, ""},
		{http.MethodGet, "/v1/issues/" + it.Key + "/tasks", ""},
		{http.MethodPost, "/v1/issues/" + it.Key + "/tasks", `{"title":"t"}`},
		{http.MethodPatch, "/v1/tasks/" + task.Key, `{"status":"done"}`},
	}
	for _, c := range foreign {
		rec := serve(mux, request(c.method, c.path, c.body, "issue-http-a"))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: want 404 for another user's row, got %d (%s)", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	// B still sees her own rows
	rec := serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key, "", "issue-http-b"))
	if rec.Code != http.StatusOK {
		t.Fatalf("owner must still read her own issue, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandler_SessionOwnershipAndOrigin(t *testing.T) {
	st := newTestStore(t)
	mux := newTestMux(st)
	userA, userB := "issue-http-sess-a", "issue-http-sess-b"
	insertUser(t, st, userA)
	insertUser(t, st, userB)
	insertAssistant(t, st, "asst-http-a", userA)
	insertSession(t, st, "sess-http-a", userA, "asst-http-a")
	insertSession(t, st, "sess-http-b", userB, "")

	// console path: no sessionId means origin console and no attribution
	rec := serve(mux, request(http.MethodPost, "/v1/issues", `{"title":"控制台议题"}`, userA))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created struct {
		Issue Issue `json:"issue"`
	}
	decodeInto(t, rec, &created)
	if created.Issue.Origin != OriginConsole || created.Issue.SessionID != "" || created.Issue.AssistantID != "" {
		t.Fatalf("console create: want origin console with no attribution, got %+v", created.Issue)
	}

	// a foreign or unknown sessionId is a client mistake (400), not a hint that
	// the id exists, and it writes nothing
	for _, sid := range []string{"sess-http-b", "sess-http-missing"} {
		rec := serve(mux, request(http.MethodPost, "/v1/issues",
			fmt.Sprintf(`{"title":"冒名议题","sessionId":%q}`, sid), userA))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("sessionId %s: want 400, got %d (%s)", sid, rec.Code, rec.Body.String())
		}
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues", "", userA))
	var listed struct {
		Issues []Issue `json:"issues"`
	}
	decodeInto(t, rec, &listed)
	if len(listed.Issues) != 1 {
		t.Fatalf("rejected creates must not write rows, got %d issues", len(listed.Issues))
	}

	// the caller's own session: origin chat, session stored, assistantId derived
	// from the session row (never taken from the request)
	rec = serve(mux, request(http.MethodPost, "/v1/issues",
		`{"title":"助手议题","assistantId":"asst-forged","sessionId":"sess-http-a"}`, userA))
	if rec.Code != http.StatusCreated {
		t.Fatalf("chat create: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	decodeInto(t, rec, &created)
	if created.Issue.Origin != OriginChat || created.Issue.SessionID != "sess-http-a" {
		t.Fatalf("chat create: want chat origin on sess-http-a, got %+v", created.Issue)
	}
	if created.Issue.AssistantID != "asst-http-a" {
		t.Fatalf("assistantId must come from the session row, got %q", created.Issue.AssistantID)
	}
	issueKey := created.Issue.Key

	// docs: foreign session rejected, own session recorded with the row's assistant
	rec = serve(mux, request(http.MethodPost, "/v1/issues/"+issueKey+"/docs",
		`{"kind":"spec","contentMd":"# Spec","sessionId":"sess-http-b"}`, userA))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("doc with foreign sessionId: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = serve(mux, request(http.MethodPost, "/v1/issues/"+issueKey+"/docs",
		`{"kind":"spec","contentMd":"# Spec","authorType":"user","sessionId":"sess-http-a"}`, userA))
	if rec.Code != http.StatusCreated {
		t.Fatalf("doc create: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var docOut struct {
		Doc Doc `json:"doc"`
	}
	decodeInto(t, rec, &docOut)
	if docOut.Doc.SessionID != "sess-http-a" || docOut.Doc.AssistantID != "asst-http-a" {
		t.Fatalf("doc attribution: got session=%q assistant=%q", docOut.Doc.SessionID, docOut.Doc.AssistantID)
	}
	if docOut.Doc.AuthorType != "user" {
		t.Fatalf("authorType must come from the request, got %q", docOut.Doc.AuthorType)
	}
	rec = serve(mux, request(http.MethodPost, "/v1/issues/"+issueKey+"/docs",
		`{"kind":"plan","contentMd":"# Plan"}`, userA))
	decodeInto(t, rec, &docOut)
	if docOut.Doc.AuthorType != "assistant" {
		t.Fatalf("authorType defaults to assistant, got %q", docOut.Doc.AuthorType)
	}

	// tasks: same rule on create and on patch
	rec = serve(mux, request(http.MethodPost, "/v1/issues/"+issueKey+"/tasks",
		`{"title":"任务","sessionId":"sess-http-b"}`, userA))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("task with foreign sessionId: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = serve(mux, request(http.MethodPost, "/v1/issues/"+issueKey+"/tasks",
		`{"title":"任务","sessionId":"sess-http-a"}`, userA))
	if rec.Code != http.StatusCreated {
		t.Fatalf("task create: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var taskOut struct {
		Task Task `json:"task"`
	}
	decodeInto(t, rec, &taskOut)
	if taskOut.Task.SessionID != "sess-http-a" || taskOut.Task.AssistantID != "asst-http-a" {
		t.Fatalf("task attribution: got session=%q assistant=%q", taskOut.Task.SessionID, taskOut.Task.AssistantID)
	}
	rec = serve(mux, request(http.MethodPatch, "/v1/tasks/"+taskOut.Task.Key,
		`{"sessionId":"sess-http-b"}`, userA))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("task patch with foreign sessionId: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = serve(mux, request(http.MethodPatch, "/v1/tasks/"+taskOut.Task.Key,
		`{"status":"in_progress","sessionId":"sess-http-a"}`, userA))
	if rec.Code != http.StatusOK {
		t.Fatalf("task patch: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	decodeInto(t, rec, &taskOut)
	if taskOut.Task.Status != TaskInProgress || taskOut.Task.SessionID != "sess-http-a" {
		t.Fatalf("task patch: got status=%q session=%q", taskOut.Task.Status, taskOut.Task.SessionID)
	}
}

// TestHandler_RoundTrip drives the console flow over HTTP and pins every
// envelope the web client is written against.
func TestHandler_RoundTrip(t *testing.T) {
	st := newTestStore(t)
	mux := newTestMux(st)
	user := "issue-http-rt"
	insertUser(t, st, user)

	rec := serve(mux, request(http.MethodPost, "/v1/issues", `{"title":"HTTP 议题","summary":"概要"}`, user))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var it Issue
	if err := json.Unmarshal(singleEnvelope(t, rec, "issue"), &it); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(it.Key, "ISS-") || it.Status != StatusDrafting || it.Origin != OriginConsole {
		t.Fatalf("new issue: %+v", it)
	}
	if it.Summary != "概要" {
		t.Fatalf("summary must round-trip, got %q", it.Summary)
	}

	// list envelope, with and without the status filter
	rec = serve(mux, request(http.MethodGet, "/v1/issues", "", user))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var list []Issue
	if err := json.Unmarshal(singleEnvelope(t, rec, "issues"), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Key != it.Key {
		t.Fatalf("list: %+v", list)
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues?status=drafting", "", user))
	var filtered []Issue
	if err := json.Unmarshal(singleEnvelope(t, rec, "issues"), &filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 {
		t.Fatalf("status filter dropped a match: %s", rec.Body.String())
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues?status=done", "", user))
	if raw := singleEnvelope(t, rec, "issues"); string(raw) != "[]" {
		t.Fatalf("empty list must marshal as [], got %s", raw)
	}

	// detail envelope: issue + doc index + tasks, empty lists as []
	rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key, "", user))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var detail struct {
		Issue Issue  `json:"issue"`
		Docs  []Doc  `json:"docs"`
		Tasks []Task `json:"tasks"`
	}
	decodeInto(t, rec, &detail)
	if detail.Issue.ID != it.ID || len(detail.Docs) != 0 || len(detail.Tasks) != 0 {
		t.Fatalf("detail: %s", rec.Body.String())
	}
	var rawDetail map[string]json.RawMessage
	decodeInto(t, rec, &rawDetail)
	if string(rawDetail["docs"]) != "[]" || string(rawDetail["tasks"]) != "[]" {
		t.Fatalf("empty sub-lists must marshal as [], got %s", rec.Body.String())
	}

	// write a current spec: version 1, status advances to specced
	rec = serve(mux, request(http.MethodPost, "/v1/issues/"+it.Key+"/docs",
		`{"kind":"spec","title":"规格","contentMd":"# Spec","status":"current"}`, user))
	if rec.Code != http.StatusCreated {
		t.Fatalf("write doc: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var doc Doc
	if err := json.Unmarshal(singleEnvelope(t, rec, "doc"), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Status != DocCurrent || doc.ContentMD != "# Spec" {
		t.Fatalf("written doc: %+v", doc)
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key, "", user))
	decodeInto(t, rec, &detail)
	if detail.Issue.Status != StatusSpecced {
		t.Fatalf("current spec must advance the issue, got %s", detail.Issue.Status)
	}

	// doc index carries no bodies; the single-doc read does
	rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key+"/docs", "", user))
	if rec.Code != http.StatusOK {
		t.Fatalf("list docs: %d (%s)", rec.Code, rec.Body.String())
	}
	var docs []Doc
	if err := json.Unmarshal(singleEnvelope(t, rec, "docs"), &docs); err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Key != doc.Key || docs[0].ContentMD != "" {
		t.Fatalf("doc index: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "contentMd") {
		t.Fatalf("doc index must omit bodies: %s", rec.Body.String())
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key+"/docs?kind=plan", "", user))
	if raw := singleEnvelope(t, rec, "docs"); string(raw) != "[]" {
		t.Fatalf("kind filter must drop the spec, got %s", raw)
	}
	for _, docKey := range []string{"latest", doc.Key, doc.ID} {
		rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key+"/docs/"+docKey, "", user))
		if rec.Code != http.StatusOK {
			t.Fatalf("get doc %s: want 200, got %d (%s)", docKey, rec.Code, rec.Body.String())
		}
		var got Doc
		if err := json.Unmarshal(singleEnvelope(t, rec, "doc"), &got); err != nil {
			t.Fatal(err)
		}
		if got.ID != doc.ID || got.ContentMD != "# Spec" {
			t.Fatalf("get doc %s: %+v", docKey, got)
		}
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key+"/docs/DOC-990001", "", user))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown docKey: want 404, got %d (%s)", rec.Code, rec.Body.String())
	}

	// tasks: create, list, patch, and the issue status that follows
	rec = serve(mux, request(http.MethodPost, "/v1/issues/"+it.Key+"/tasks",
		`{"title":"实现 HTTP 层","detail":"细节","position":1}`, user))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var task Task
	if err := json.Unmarshal(singleEnvelope(t, rec, "task"), &task); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(task.Key, "TSK-") || task.Status != TaskTodo || task.Position != 1 {
		t.Fatalf("created task: %+v", task)
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key+"/tasks", "", user))
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks: %d (%s)", rec.Code, rec.Body.String())
	}
	var tasks []Task
	if err := json.Unmarshal(singleEnvelope(t, rec, "tasks"), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Key != task.Key {
		t.Fatalf("task list: %s", rec.Body.String())
	}

	rec = serve(mux, request(http.MethodPatch, "/v1/tasks/"+task.Key, `{"status":"in_progress"}`, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch task: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(singleEnvelope(t, rec, "task"), &task); err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskInProgress {
		t.Fatalf("patched task: %+v", task)
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key, "", user))
	decodeInto(t, rec, &detail)
	if detail.Issue.Status != StatusInProgress {
		t.Fatalf("started task must move the issue to in_progress, got %s", detail.Issue.Status)
	}

	rec = serve(mux, request(http.MethodPatch, "/v1/tasks/"+task.Key, `{"status":"done"}`, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete task: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = serve(mux, request(http.MethodGet, "/v1/issues/"+it.Key, "", user))
	decodeInto(t, rec, &detail)
	if detail.Issue.Status != StatusDone || detail.Issue.ClosedAt == nil {
		t.Fatalf("all tasks done must close the issue, got %s closed=%v", detail.Issue.Status, detail.Issue.ClosedAt)
	}

	// the explicit PATCH is the user's call and keeps closed_at in step
	rec = serve(mux, request(http.MethodPatch, "/v1/issues/"+it.Key, `{"status":"in_progress"}`, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch issue: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(singleEnvelope(t, rec, "issue"), &it); err != nil {
		t.Fatal(err)
	}
	if it.Status != StatusInProgress || it.ClosedAt != nil {
		t.Fatalf("reopened issue: %+v", it)
	}
	rec = serve(mux, request(http.MethodPatch, "/v1/issues/"+it.Key, `{"status":"cancelled","title":"改名"}`, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel issue: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(singleEnvelope(t, rec, "issue"), &it); err != nil {
		t.Fatal(err)
	}
	if it.Status != StatusCancelled || it.Title != "改名" || it.ClosedAt == nil {
		t.Fatalf("cancelled issue: %+v", it)
	}
}
