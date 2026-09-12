package assistant

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

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

func TestStore_CreateListUpdate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := "asst-" + uuid.NewString()[:8]
	insertUser(t, db, user)

	store := &Store{DB: db.SQL}
	a, err := store.Create(ctx, user, CreateInput{
		Name:         "Ada",
		Bio:          "backend CI",
		IdentityMode: IdentityProxyUser,
		Preset:       "code_browser",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == "" || !a.Capabilities.Shell || !a.Capabilities.Browser {
		t.Fatalf("create result: %+v", a)
	}
	if a.NetworkTier != NetworkDevSites {
		t.Fatalf("default network: %s", a.NetworkTier)
	}

	got, err := store.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Ada" || got.Bio != "backend CI" {
		t.Fatalf("get: %+v", got)
	}

	list, err := store.ListByUser(ctx, user)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("list: %+v", list)
	}

	bio := "updated bio"
	tier := NetworkNone
	updated, err := store.Update(ctx, a.ID, UpdateInput{Bio: &bio, NetworkTier: &tier})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Bio != bio || updated.NetworkTier != NetworkNone {
		t.Fatalf("update result: %+v", updated)
	}

	disabled := StatusDisabled
	if _, err := store.Update(ctx, a.ID, UpdateInput{Status: &disabled}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	list, err = store.ListByUser(ctx, user)
	if err != nil {
		t.Fatalf("list after disable: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("disabled should be hidden: %+v", list)
	}
}

func TestStore_AttachOrphanSessions(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := "orphan-" + uuid.NewString()[:8]
	insertUser(t, db, user)

	astore := &Store{DB: db.SQL}
	a, err := astore.Create(ctx, user, CreateInput{
		Name: "Default", IdentityMode: IdentityProxyUser, Preset: "code",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO agent_sessions (id, user_id, title, provider_id, sandbox_id, status, created_at, updated_at)
		VALUES ($1,$2,'old','sysadmin','','active',now(),now())`,
		uuid.NewString(), user,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	n, err := astore.AttachOrphanSessions(ctx, user, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("attached=%d want 1", n)
	}
}

func TestStore_EnsureSystemAndUndeletable(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := "sys-" + uuid.NewString()[:8]
	insertUser(t, db, user)

	store := &Store{DB: db.SQL}
	a, err := store.EnsureSystem(ctx, user)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if a.Kind != KindSystem || a.Name != DefaultSystemName {
		t.Fatalf("system: %+v", a)
	}
	again, err := store.EnsureSystem(ctx, user)
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if again.ID != a.ID {
		t.Fatalf("not idempotent: %s vs %s", again.ID, a.ID)
	}

	userAsst, err := store.Create(ctx, user, CreateInput{
		Name: "Temp", IdentityMode: IdentityProxyUser, Preset: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.ListByUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 || list[0].Kind != KindSystem {
		t.Fatalf("system should sort first: %+v", list)
	}

	disabled := StatusDisabled
	if _, err := store.Update(ctx, a.ID, UpdateInput{Status: &disabled}); !errors.Is(err, ErrSystemUndeletable) {
		t.Fatalf("disable system: %v", err)
	}
	if _, err := store.Update(ctx, userAsst.ID, UpdateInput{Status: &disabled}); err != nil {
		t.Fatalf("disable user: %v", err)
	}
}
