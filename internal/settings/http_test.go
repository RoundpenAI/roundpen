package settings_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestAdminBrowserTestHTTP(t *testing.T) {
	cfg := &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderDocker, Port: 3000}}
	svc := settings.NewService(nil, cfg, settings.RuntimeDeps{}, settings.FromConfig(cfg))

	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	if err := users.Upsert(context.Background(), storage.User{
		Username: "root", APIKey: "rp-admin", Role: storage.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&settings.Handler{Svc: svc}).Mount(mux)
	handler := auth.Middleware(users, sessions)(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/settings/browser/test", nil)
	req.Header.Set("X-API-Key", "rp-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		OK     bool                       `json:"ok"`
		Error  string                     `json:"error"`
		Result settings.BrowserTestResult `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body=%s err=%v", rec.Body.String(), err)
	}
	if !got.OK || got.Error != "" {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if got.Result.Provider != config.CDPProviderDocker {
		t.Fatalf("provider=%q", got.Result.Provider)
	}
	if !strings.Contains(got.Result.Endpoint, "3000") {
		t.Fatalf("endpoint=%q", got.Result.Endpoint)
	}
}

func TestAdminSettingsHTTP(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	cfg := &config.Config{
		DefaultImage:    "ghcr.io/roundpenai/code-agent:0.1.0",
		DefaultTTL:      30 * time.Minute,
		PreviewTokenTTL: 15 * time.Minute,
		Backend:         "docker",
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

func TestBrowserTestManagedUsesProbe(t *testing.T) {
	cfg := &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderDocker, Port: 3000}}
	ctx := context.Background()

	// Ready host: the managed branch reports the container endpoint.
	svc := settings.NewService(nil, cfg,
		settings.RuntimeDeps{Probe: &runtime.Probe{Cfg: cfg, DockerReady: true}},
		settings.FromConfig(cfg))
	res, err := svc.TestBrowser(ctx)
	if err != nil {
		t.Fatalf("ready probe: err=%v", err)
	}
	if res.Provider != config.CDPProviderDocker || !strings.Contains(res.Endpoint, "3000") {
		t.Fatalf("result=%+v", res)
	}

	// Broken host: the button must surface the probe reason, not ok:true.
	svc = settings.NewService(nil, cfg,
		settings.RuntimeDeps{Probe: &runtime.Probe{Cfg: cfg}},
		settings.FromConfig(cfg))
	if _, err := svc.TestBrowser(ctx); err == nil {
		t.Fatal("docker down: expected error")
	} else if !strings.Contains(err.Error(), "Docker") {
		t.Fatalf("err=%v", err)
	}
}
