package settingitems_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

func testDB(t *testing.T) *storage.DB {
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
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM setting_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM setting_bindings`); err != nil {
		t.Fatal(err)
	}
	return db
}

func pgCatalog(t *testing.T, db *storage.DB, box *secretbox.Box) *settingitems.Catalog {
	t.Helper()
	reg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{userenv.ProxyKind()},
		userenv.ProxySlots(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewPGStore(db.SQL, box), reg)
	if err := cat.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	return cat
}

func TestPGSealsSecretsAtRest(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	box, err := secretbox.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	cat := pgCatalog(t, db, box)
	const url = "socks5://user:pass@10.0.0.9:1080"
	if _, err := cat.Save(ctx, settingitems.Item{
		Kind: settingitems.KindProxy, ID: "us", Name: "US egress", Enabled: true,
		Config: map[string]any{"url": url},
	}); err != nil {
		t.Fatal(err)
	}

	// The stored row must never contain the plaintext URL.
	var raw string
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT secrets::text FROM setting_items WHERE kind='proxy' AND id='us'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "enc:v1:") || strings.Contains(raw, "10.0.0.9") {
		t.Fatalf("secret not sealed at rest: %s", raw)
	}

	it, ok := cat.Snapshot().Item(settingitems.KindProxy, "us")
	if !ok || it.Secret("url") != url {
		t.Fatalf("decrypted value mismatch: %+v", it.Secrets)
	}
	if err := cat.SetBinding(ctx, settingitems.Binding{
		Scope: settingitems.UserScope("bob"), Slot: settingitems.SlotProxyAgent, ItemID: "us",
	}); err != nil {
		t.Fatal(err)
	}

	// A second catalog (another daemon on the same database) resolves the same
	// selection.
	fresh := pgCatalog(t, db, box)
	if res := fresh.Snapshot().Resolve(settingitems.SlotProxyAgent, "bob"); !res.OK ||
		res.Item.Secret("url") != url {
		t.Fatalf("fresh snapshot: %+v", res)
	}

	// Deleting the item cascades into the binding.
	if err := cat.Delete(ctx, settingitems.KindProxy, "us", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := cat.Snapshot().Binding(settingitems.UserScope("bob"), settingitems.SlotProxyAgent); ok {
		t.Fatal("binding survived the item delete")
	}
}
