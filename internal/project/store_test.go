package project

import (
	"context"
	"os"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func newTestStores(t *testing.T) (*Store, *assistant.Store) {
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
	return &Store{DB: db.SQL}, &assistant.Store{DB: db.SQL}
}

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

func TestStore_ProjectLifecycle(t *testing.T) {
	st, asst := newTestStores(t)
	ctx := context.Background()
	user := "project-test-user"
	insertUser(t, st, user)

	coord, err := asst.Create(ctx, user, assistant.DefaultCoordinatorCreateInput("Demo"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.Create(ctx, user, CreateInput{Name: "Demo App", RepoURL: "https://github.com/acme/demo.git"}, coord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key == "" || p.Slug != "demo-app" || p.RepoPath != "projects/demo-app" {
		t.Fatalf("unexpected project: %+v", p)
	}
	if p.CoordinatorAssistantID != coord.ID {
		t.Fatalf("coordinator: %q", p.CoordinatorAssistantID)
	}

	got, err := st.Get(ctx, user, p.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Demo App" {
		t.Fatalf("name: %q", got.Name)
	}

	list, err := st.List(ctx, user, ListFilter{Status: StatusActive})
	if err != nil || len(list) < 1 {
		t.Fatalf("list: %v %#v", err, list)
	}

	name := "Demo App 2"
	updated, err := st.Update(ctx, user, p.Key, UpdateInput{Name: &name})
	if err != nil || updated.Name != name {
		t.Fatalf("update: %+v %v", updated, err)
	}

	// Coordinators must not appear in the assistants sidebar list.
	assistants, err := asst.ListByUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range assistants {
		if a.Kind == assistant.KindCoordinator {
			t.Fatalf("coordinator leaked into ListByUser: %+v", a)
		}
	}

	_, err = st.Create(ctx, user, CreateInput{Name: "Other", Slug: "demo-app"}, coord.ID)
	if err != ErrSlugTaken {
		t.Fatalf("want ErrSlugTaken, got %v", err)
	}
}
