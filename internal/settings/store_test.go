package settings_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
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
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM app_settings`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBootstrapSeedsFromConfig(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	store := settings.NewStore(db.SQL, nil)
	cfg := &config.Config{
		AllowPublicRegistration: true,
		DefaultImage:            "base",
		DefaultTTL:              45 * time.Minute,
		PreviewPublicURL:        "http://preview.test",
		PreviewTokenTTL:         10 * time.Minute,
	}

	got, err := settings.Bootstrap(ctx, store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AllowPublicRegistration || got.DefaultImage != "base" {
		t.Fatalf("seed mismatch: %+v", got)
	}
	if got.DefaultTtlSeconds != 2700 {
		t.Fatalf("ttl seconds: %d", got.DefaultTtlSeconds)
	}

	exists, err := store.Exists(ctx)
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
}

func TestBootstrapLoadsDBOverrides(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	store := settings.NewStore(db.SQL, nil)
	envCfg := &config.Config{
		DefaultImage:    "host",
		DefaultTTL:      30 * time.Minute,
		PreviewTokenTTL: 15 * time.Minute,
	}
	if _, err := settings.Bootstrap(ctx, store, envCfg); err != nil {
		t.Fatal(err)
	}

	dbCfg := &config.Config{
		DefaultImage:    "host",
		DefaultTTL:      30 * time.Minute,
		PreviewTokenTTL: 15 * time.Minute,
	}
	want := settings.AppSettings{
		DefaultImage:      "python",
		DefaultTtlSeconds: 1200,
	}
	if err := store.Upsert(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := settings.Bootstrap(ctx, store, dbCfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultImage != "python" || got.DefaultTtlSeconds != 1200 {
		t.Fatalf("loaded: %+v", got)
	}
	if dbCfg.DefaultImage != "python" || dbCfg.DefaultTTL != 20*time.Minute {
		t.Fatalf("cfg not updated: image=%q ttl=%v", dbCfg.DefaultImage, dbCfg.DefaultTTL)
	}
}

func TestVerifySecretsRejectsForeignKey(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	box, err := secretbox.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	other, err := secretbox.New(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := settings.NewStore(db.SQL, box)
	if err := store.Upsert(ctx, settings.AppSettings{
		DefaultImage:      "host",
		DefaultTtlSeconds: 1800,
		LlmgwVirtualKeys:  "vk-devsecret:dev",
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.VerifySecrets(ctx); err != nil {
		t.Fatalf("matching key must verify: %v", err)
	}
	// A wrong key (usually a different ROUNDPEN_DATA_ROOT) must be reported at
	// startup, before a read blanks the field and a save persists the blank.
	if err := settings.NewStore(db.SQL, other).VerifySecrets(ctx); err == nil ||
		!strings.Contains(err.Error(), "app_settings") {
		t.Fatalf("wrong key must fail verification: %v", err)
	}
}

func TestUpsertEncryptsSecretsAtRest(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	box, err := secretbox.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := settings.NewStore(db.SQL, box)

	in := settings.AppSettings{
		DefaultImage:      "host",
		DefaultTtlSeconds: 1800,
		LlmgwVirtualKeys:  "vk-devsecret:dev",
	}
	if err := store.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}

	var raw []byte
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT payload FROM app_settings WHERE id='global'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "vk-devsecret") {
		t.Fatalf("plaintext virtual key in payload: %s", body)
	}
	if !strings.Contains(body, "enc:v1:") {
		t.Fatalf("expected sealed values in payload: %s", body)
	}

	got, err := store.Load(ctx, settings.AppSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if got.LlmgwVirtualKeys != "vk-devsecret:dev" {
		t.Fatalf("decrypted mismatch: %+v", got)
	}

	// A legacy row written without encryption still loads (plaintext passthrough).
	legacyStore := settings.NewStore(db.SQL, nil)
	if err := legacyStore.Upsert(ctx, got); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.Load(ctx, settings.AppSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.LlmgwVirtualKeys != "vk-devsecret:dev" {
		t.Fatalf("legacy plaintext load mismatch: %+v", legacy)
	}
}
