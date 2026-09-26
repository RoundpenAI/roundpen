package preview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// testClaimStore opens the integration database, or skips when the suite runs
// without one (same contract as the other store tests).
func testClaimStore(t *testing.T) *ClaimStore {
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
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM preview_domains WHERE name LIKE 'rptest-%'`); err != nil {
		t.Fatal(err)
	}
	return &ClaimStore{DB: db.SQL}
}

func TestClaimStoreRoundTrip(t *testing.T) {
	store := testClaimStore(t)
	ctx := context.Background()
	name := fmt.Sprintf("rptest-%d", time.Now().UnixNano())

	if err := store.Claim(ctx, Claim{Name: name, SandboxID: "sb-1", Port: 3000, Owner: "alice"}); err != nil {
		t.Fatal(err)
	}
	if id, port, ok := store.Resolve(ctx, name); !ok || id != "sb-1" || port != 3000 {
		t.Fatalf("resolve = %q %d %v", id, port, ok)
	}

	// The owner moves the name to a rebuilt workspace instead of losing it.
	if err := store.Claim(ctx, Claim{Name: name, SandboxID: "sb-2", Port: 8080, Owner: "alice"}); err != nil {
		t.Fatal(err)
	}
	if id, port, ok := store.Resolve(ctx, name); !ok || id != "sb-2" || port != 8080 {
		t.Fatalf("after rebind = %q %d %v", id, port, ok)
	}

	// Somebody else cannot take it, not even to their own port.
	if err := store.Claim(ctx, Claim{Name: name, SandboxID: "sb-9", Port: 3000, Owner: "bob"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("second owner: err = %v, want ErrNameTaken", err)
	}

	claims, err := store.List(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range claims {
		if c.Name == name {
			found = true
			if c.SandboxID != "sb-2" || c.Owner != "alice" {
				t.Fatalf("listed claim = %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("claim missing from the owner's list")
	}

	if released, err := store.Release(ctx, name, "bob"); err != nil || released {
		t.Fatalf("foreign release: %v %v", released, err)
	}
	if released, err := store.Release(ctx, name, "alice"); err != nil || !released {
		t.Fatalf("owner release: %v %v", released, err)
	}
	if _, _, ok := store.Resolve(ctx, name); ok {
		t.Fatal("released name still resolves")
	}
	if released, _ := store.Release(ctx, name, "alice"); released {
		t.Fatal("second release must report nothing to do")
	}
}
