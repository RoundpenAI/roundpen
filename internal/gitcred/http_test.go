package gitcred

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// pgHandler wires a Handler against the dedicated test database.
func pgHandler(t *testing.T) (*Handler, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ROUNDPEN_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set ROUNDPEN_TEST_DATABASE_URL to a dedicated test database")
	}
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Handler{Store: &Store{DB: db.SQL}}, db.SQL
}

// addUser inserts the owner row the credential's foreign key needs.
func addUser(t *testing.T, db *sql.DB, username string) {
	t.Helper()
	users := storage.NewUserStore(&storage.DB{SQL: db})
	if err := users.Upsert(context.Background(), storage.User{
		Username: username, APIKey: "rp-" + username, Role: storage.RoleUser,
	}); err != nil {
		t.Fatalf("user upsert: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.ExecContext(ctx, `DELETE FROM user_git_credentials WHERE user_id=$1`, username)
		_ = users.Delete(ctx, username)
	})
}

func requestAs(method, path, body, user string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	return r.WithContext(auth.WithUser(r.Context(), &storage.User{Username: user}))
}

func serve(mux *http.ServeMux, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestHandlerNotifiesOnUpsertAndDelete(t *testing.T) {
	h, db := pgHandler(t)
	const user = "gitcred-hook-user"
	addUser(t, db, user)

	var changed []string
	h.OnChange = func(_ context.Context, userID string) { changed = append(changed, userID) }

	mux := http.NewServeMux()
	h.Mount(mux)

	rec := serve(mux, requestAs(http.MethodPut, "/v1/me/git-credentials",
		`{"provider":"gitea","host":"git.example.test","token":"tok"}`, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status = %d (%s)", rec.Code, rec.Body.String())
	}
	var cred Cred
	if err := json.Unmarshal(rec.Body.Bytes(), &cred); err != nil {
		t.Fatal(err)
	}

	if rec := serve(mux, requestAs(http.MethodDelete, "/v1/me/git-credentials/"+cred.ID, "", user)); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d (%s)", rec.Code, rec.Body.String())
	}
	// Deleting a row that is already gone changed nothing and must not re-inject.
	if rec := serve(mux, requestAs(http.MethodDelete, "/v1/me/git-credentials/"+cred.ID, "", user)); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d", rec.Code)
	}

	if len(changed) != 2 || changed[0] != user || changed[1] != user {
		t.Errorf("changed = %v, want two notifications for %s", changed, user)
	}
	if got, err := h.Store.List(context.Background(), user); err != nil || len(got) != 0 {
		t.Errorf("credential left behind: %+v (%v)", got, err)
	}
}
