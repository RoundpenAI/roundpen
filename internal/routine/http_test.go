package routine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func newTestMux(st *Store, asst *assistant.Store) *http.ServeMux {
	mux := http.NewServeMux()
	(&Handler{Store: st, Assistants: asst}).Mount(mux)
	return mux
}

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

func TestHandler_RequiresAuth(t *testing.T) {
	mux := newTestMux(nil, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(http.MethodGet, "/v1/routines", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHandler_CreateChecksCapabilityAndOwner(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-http")
	insertUser(t, st, "routine-http-other")
	if _, err := st.DB.ExecContext(ctx, `
		INSERT INTO assistants (id, user_id, name, capabilities, network_tier)
		VALUES ('asst-nobrowser', 'routine-http', 'no-browser',
			'{"shell":true,"browser":false,"mobile":false,"desktop":false}', 'all')`); err != nil {
		t.Fatal(err)
	}
	insertAssistant(t, st, "asst-http", "routine-http")
	insertAssistant(t, st, "asst-other", "routine-http-other")
	mux := newTestMux(st, &assistant.Store{DB: st.DB})

	body := `{"title":"券","autonomy":"browse","cron":"0 9 * * *","timezone":"UTC","hosts":["shop.example"],"assigneeAssistantId":"asst-nobrowser","assigneeConfirmed":true}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(http.MethodPost, "/v1/routines", body, "routine-http"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("browse without browser: %d %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := st.DB.QueryRowContext(ctx, `SELECT count(*) FROM routines WHERE user_id='routine-http'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rejected create still inserted %d", n)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, request(http.MethodPost, "/v1/routines",
		`{"title":"周报","autonomy":"read","cron":"0 8 * * 1","timezone":"UTC","assigneeAssistantId":"asst-other","assigneeConfirmed":true}`,
		"routine-http"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-user assignee: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, request(http.MethodPost, "/v1/routines",
		`{"title":"周报","autonomy":"read","cron":"0 8 * * 1","timezone":"UTC","assigneeAssistantId":"asst-http"}`,
		"routine-http"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed assignee: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, request(http.MethodPost, "/v1/routines",
		`{"title":"周报","autonomy":"read","cron":"0 8 * * 1","timezone":"UTC","assigneeAssistantId":"asst-http","assigneeConfirmed":true}`,
		"routine-http"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, request(http.MethodGet, "/v1/routines", "", "routine-http-other"))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "RTN-") {
		t.Fatalf("other user list: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDisableAssistantPausesRoutines(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-disable")
	insertAssistant(t, st, "asst-disable", "routine-disable")
	rt, err := st.Create(ctx, "routine-disable", CreateInput{
		AssigneeAssistantID: "asst-disable", Title: "周报", Autonomy: AutonomyRead,
		Cron: "0 8 * * 1", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&assistant.Handler{
		Store: &assistant.Store{DB: st.DB},
		OnDisabled: func(ctx context.Context, assistantID string) {
			_ = st.PauseByAssignee(ctx, assistantID)
		},
	}).Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request(http.MethodPatch, "/v1/assistants/asst-disable", `{"status":"disabled"}`, "routine-disable"))
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	got, _, err := st.Get(ctx, "routine-disable", rt.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPaused || got.NextRunAt != nil {
		t.Fatalf("after disable status=%s next=%v", got.Status, got.NextRunAt)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, request(http.MethodPatch, "/v1/assistants/asst-disable", `{"status":"active"}`, "routine-disable"))
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body.String())
	}
	got, _, err = st.Get(ctx, "routine-disable", rt.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPaused {
		t.Fatalf("re-enabling the assistant resumed the routine: %s", got.Status)
	}
}
