package nasscene_test

import (
	"context"
	"os"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/nasscene"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func testDB(t *testing.T) *storage.DB {
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
	return db
}

func insertUser(t *testing.T, db *storage.DB, user string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO users (username, email, role, api_key, created_at, updated_at)
		VALUES ($1, $2, 'user', $3, now(), now())
		ON CONFLICT (username) DO NOTHING`,
		user, user+"@example.com", "rp-"+user,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM users WHERE username=$1`, user)
	})
}

func TestEnabledDefaultsByAuthProvider(t *testing.T) {
	db := testDB(t)
	user := "nas-scene-" + t.Name()
	insertUser(t, db, user)
	asst := &assistant.Store{DB: db.SQL}
	store := &nasscene.Store{DB: db.SQL, Assistants: asst}

	on, err := store.Enabled(context.Background(), user, nasscene.AuthProviderFnOS)
	if err != nil || !on {
		t.Fatalf("fnos default: on=%v err=%v", on, err)
	}
	on, err = store.Enabled(context.Background(), user, "local")
	if err != nil || on {
		t.Fatalf("local default: on=%v err=%v", on, err)
	}
}

func TestEnsureAndDisable(t *testing.T) {
	db := testDB(t)
	user := "nas-scene-ensure"
	insertUser(t, db, user)
	asst := &assistant.Store{DB: db.SQL}
	store := &nasscene.Store{DB: db.SQL, Assistants: asst}
	ctx := context.Background()

	if err := store.SetEnabled(ctx, user, true); err != nil {
		t.Fatal(err)
	}
	list, err := asst.ListByUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range list {
		if a.Kind == assistant.KindUser {
			n++
		}
	}
	if n < len(nasscene.Templates()) {
		t.Fatalf("want ≥%d user assistants, got %d (list=%d)", len(nasscene.Templates()), n, len(list))
	}

	st, err := store.Describe(ctx, user, "local")
	if err != nil || !st.Enabled {
		t.Fatalf("describe: %+v err=%v", st, err)
	}
	for _, m := range st.Members {
		if m.Status != assistant.StatusActive || m.AssistantID == "" {
			t.Fatalf("member %+v", m)
		}
	}

	if err := store.SetEnabled(ctx, user, false); err != nil {
		t.Fatal(err)
	}
	list, err = asst.ListByUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		for _, tmpl := range nasscene.Templates() {
			if a.Name == tmpl.Name {
				t.Fatalf("disabled pack assistant still listed: %s", a.Name)
			}
		}
	}
}
