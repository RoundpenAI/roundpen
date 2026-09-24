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
	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestFromConfigSeedsAutoModeDefaults(t *testing.T) {
	got := settings.FromConfig(&config.Config{})
	for name, list := range map[string][]string{
		"environment": got.AutoMode.Environment,
		"allow":       got.AutoMode.Allow,
		"softDeny":    got.AutoMode.SoftDeny,
		"hardDeny":    got.AutoMode.HardDeny,
	} {
		if len(list) != 1 || list[0] != automode.DefaultsToken {
			t.Fatalf("%s = %v, want [$defaults]", name, list)
		}
	}
}

func TestDecodeAppSettingsKeepsFallbackAutoMode(t *testing.T) {
	fallback := settings.FromConfig(&config.Config{})
	fallback.AutoMode.Allow = []string{"Existing custom rule"}
	got, err := settings.DecodeAppSettings([]byte(`{
		"defaultImage": "host",
		"defaultTtlSeconds": 1800,
		"previewTokenTtlSeconds": 900
	}`), fallback)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AutoMode.Allow) != 1 || got.AutoMode.Allow[0] != "Existing custom rule" {
		t.Fatalf("fallback autoMode lost: %+v", got.AutoMode)
	}
}

func TestDecodeAppSettingsHonorsExplicitAutoMode(t *testing.T) {
	fallback := settings.FromConfig(&config.Config{})
	got, err := settings.DecodeAppSettings([]byte(`{
		"defaultImage": "host",
		"defaultTtlSeconds": 1800,
		"previewTokenTtlSeconds": 900,
		"autoMode": {"allow": ["Only mine"], "model": "fast-model"}
	}`), fallback)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AutoMode.Allow) != 1 || got.AutoMode.Allow[0] != "Only mine" {
		t.Fatalf("explicit allow not applied: %+v", got.AutoMode.Allow)
	}
	if len(got.AutoMode.Environment) != 1 || got.AutoMode.Environment[0] != automode.DefaultsToken {
		t.Fatalf("untouched list should keep fallback: %+v", got.AutoMode.Environment)
	}
}

func TestAutoModeValidateLimits(t *testing.T) {
	base := func() settings.AppSettings {
		s := settings.FromConfig(&config.Config{})
		s.DefaultImage = "host"
		s.DefaultTtlSeconds = 60
		return s
	}

	tooMany := base()
	for i := 0; i < 51; i++ {
		tooMany.AutoMode.Allow = append(tooMany.AutoMode.Allow, "rule")
	}
	if err := tooMany.Validate(); err == nil || !strings.Contains(err.Error(), "autoMode.allow") {
		t.Fatalf("expected entry-count error, got %v", err)
	}

	tooLong := base()
	tooLong.AutoMode.SoftDeny = []string{strings.Repeat("x", 801)}
	if err := tooLong.Validate(); err == nil || !strings.Contains(err.Error(), "autoMode.softDeny") {
		t.Fatalf("expected entry-length error, got %v", err)
	}

	longRule := base()
	longRule.AutoMode.Allow = []string{strings.Repeat("m", 801)}
	if err := longRule.Validate(); err == nil || !strings.Contains(err.Error(), "autoMode") {
		t.Fatalf("expected model-length error, got %v", err)
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

// TestAdminAutoModeHTTP covers the defaults viewer plus normalization of
// admin-submitted rules (whitespace, duplicates, empty entries).
func TestAdminAutoModeHTTP(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	cfg := &config.Config{DefaultImage: "host", DefaultTTL: 30 * time.Minute, PreviewTokenTTL: 15 * time.Minute}
	store := settings.NewStore(db.SQL, nil)
	current, err := settings.Bootstrap(ctx, store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := settings.NewService(store, cfg, settings.RuntimeDeps{}, current)

	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	if err := users.Upsert(ctx, storage.User{Username: "root", APIKey: "rp-admin", Role: storage.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&settings.Handler{Svc: svc}).Mount(mux)
	handler := auth.Middleware(users, sessions)(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/settings/automode/defaults", nil)
	req.Header.Set("X-API-Key", "rp-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("defaults status=%d body=%s", rec.Code, rec.Body.String())
	}
	var defaults struct {
		Environment []string `json:"environment"`
		Allow       []string `json:"allow"`
		SoftDeny    []string `json:"softDeny"`
		HardDeny    []string `json:"hardDeny"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &defaults); err != nil {
		t.Fatalf("defaults body=%s err=%v", rec.Body.String(), err)
	}
	def := automode.Defaults()
	if len(defaults.HardDeny) != len(def.HardDeny) || len(defaults.Allow) != len(def.Allow) {
		t.Fatalf("defaults mismatch: %+v", defaults)
	}

	body, _ := json.Marshal(settings.AppSettings{
		DefaultImage:      "python",
		DefaultTtlSeconds: 3600,
		AutoMode: settings.AutoModeSettings{
			Allow: []string{"  Mine  ", "", "Mine", "Another"},
		},
	})
	req = httptest.NewRequest(http.MethodPut, "/v1/admin/settings", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "rp-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Settings settings.AppSettings `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	got := envelope.Settings.AutoMode
	if len(got.Allow) != 2 || got.Allow[0] != "Mine" || got.Allow[1] != "Another" {
		t.Fatalf("allow not normalized: %#v", got.Allow)
	}
	if len(got.Environment) != 1 || got.Environment[0] != automode.DefaultsToken {
		t.Fatalf("environment lost on partial update: %#v", got.Environment)
	}
	if len(svc.Current().AutoMode.Allow) != 2 {
		t.Fatal("service snapshot not updated")
	}
}
