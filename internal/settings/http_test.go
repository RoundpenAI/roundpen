package settings_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestAdminSettingsHTTP(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	cfg := &config.Config{
		DefaultImage:    "host",
		DefaultTTL:      30 * time.Minute,
		PreviewTokenTTL: 15 * time.Minute,
		Backend:         "kern",
		DataRoot:        "./data",
		HTTPAddr:        ":9527",
	}
	store := settings.NewStore(db.SQL)
	current, err := settings.Bootstrap(ctx, store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := settings.NewService(store, cfg, settings.RuntimeDeps{}, current)

	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	if err := users.Upsert(ctx, storage.User{
		Username: "root", APIKey: "rp-admin", Role: storage.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&settings.Handler{Svc: svc}).Mount(mux)
	handler := auth.Middleware(users, sessions)(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/settings", nil)
	req.Header.Set("X-API-Key", "rp-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}

	body, _ := json.Marshal(settings.AppSettings{
		DefaultImage:           "python",
		DefaultTtlSeconds:      3600,
		PreviewTokenTtlSeconds: 900,
		TemplateBuilder:        "docker",
	})
	req = httptest.NewRequest(http.MethodPut, "/v1/admin/settings", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "rp-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cfg.DefaultImage != "python" {
		t.Fatalf("cfg not updated: %q", cfg.DefaultImage)
	}
}
