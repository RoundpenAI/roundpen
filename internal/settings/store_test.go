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
		TemplateBuilder:         "docker",
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
		DefaultImage:           "python",
		DefaultTtlSeconds:      1200,
		PreviewTokenTtlSeconds: 600,
		TemplateBuilder:        "docker",
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

func TestAppSettingsValidate(t *testing.T) {
	valid := settings.AppSettings{
		DefaultImage:           "host",
		DefaultTtlSeconds:      1800,
		PreviewTokenTtlSeconds: 900,
		TemplateBuilder:        "auto",
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (settings.AppSettings{}).Validate(); err == nil {
		t.Fatal("expected validation error")
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
		DefaultImage:           "host",
		DefaultTtlSeconds:      1800,
		PreviewTokenTtlSeconds: 900,
		LlmgwOpenaiAPIKey:      "sk-openai-secret",
		LlmgwAnthropicAPIKey:   "sk-ant-secret",
		CDPToken:               "cdp-secret",
		WebSearchApiKey:        "ws-secret",
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
	for _, secret := range []string{"sk-openai-secret", "sk-ant-secret", "cdp-secret", "ws-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("plaintext secret %q in payload", secret)
		}
	}
	if !strings.Contains(body, "enc:v1:") {
		t.Fatalf("expected sealed values in payload: %s", body)
	}

	got, err := store.Load(ctx, settings.AppSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if got.LlmgwOpenaiAPIKey != "sk-openai-secret" || got.LlmgwAnthropicAPIKey != "sk-ant-secret" ||
		got.CDPToken != "cdp-secret" || got.WebSearchApiKey != "ws-secret" {
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
	if legacy.LlmgwOpenaiAPIKey != "sk-openai-secret" || legacy.CDPToken != "cdp-secret" {
		t.Fatalf("legacy plaintext load mismatch: %+v", legacy)
	}
}
