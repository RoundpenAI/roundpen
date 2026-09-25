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
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestAgentImageValidation(t *testing.T) {
	base := func(image string) settings.AppSettings {
		return settings.AppSettings{DefaultImage: "host", DefaultTtlSeconds: 1800, AgentImage: image}
	}
	for _, image := range []string{
		"",
		"roundpen-code-agent:local",
		"ghcr.io/roundpenai/code-agent:0.1.0",
		"registry.test:5000/team/agent@sha256:" + strings.Repeat("a", 64),
	} {
		if err := base(image).Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", image, err)
		}
	}
	for _, image := range []string{
		"http://ghcr.io/roundpenai/code-agent",
		"ghcr.io/round penai/agent",
		"-rm",
		"images/agent.qcow2",
		strings.Repeat("a", 400),
	} {
		if err := base(image).Validate(); err == nil {
			t.Errorf("Validate(%q) = nil, want an error", image)
		}
	}
}

func TestUpdatePublishesAgentImage(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	cfg := &config.Config{DefaultImage: "host", DefaultTTL: 30 * time.Minute}
	store := settings.NewStore(db.SQL, nil)
	current, err := settings.Bootstrap(ctx, store, cfg)
	if err != nil {
		t.Fatal(err)
	}

	var published []string
	svc := settings.NewService(store, cfg, settings.RuntimeDeps{
		SetAgentImage: func(image string) { published = append(published, image) },
	}, current)

	next := svc.Current()
	next.AgentImage = "  ghcr.io/roundpenai/code-agent:0.2.0  "
	if err := svc.Update(ctx, next); err != nil {
		t.Fatal(err)
	}
	if len(published) != 1 || published[0] != "ghcr.io/roundpenai/code-agent:0.2.0" {
		t.Fatalf("published = %v, want the trimmed image", published)
	}
	if got := svc.Current().AgentImage; got != "ghcr.io/roundpenai/code-agent:0.2.0" {
		t.Fatalf("current agentImage = %q", got)
	}

	// A rejected value must never reach the environment service.
	bad := svc.Current()
	bad.AgentImage = "http://example.test/agent.tar"
	if err := svc.Update(ctx, bad); err == nil {
		t.Fatal("expected a validation error")
	}
	if len(published) != 1 {
		t.Fatalf("published = %v, want the invalid value withheld", published)
	}
}

func TestAdminSettingsRoundTripAgentImage(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	cfg := &config.Config{DefaultImage: "host", DefaultTTL: 30 * time.Minute}
	store := settings.NewStore(db.SQL, nil)
	current, err := settings.Bootstrap(ctx, store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var published []string
	svc := settings.NewService(store, cfg, settings.RuntimeDeps{
		SetAgentImage: func(image string) { published = append(published, image) },
	}, current)

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

	body, _ := json.Marshal(map[string]any{
		"defaultImage":            "host",
		"defaultTtlSeconds":       1800,
		"agentImage":              "ghcr.io/roundpenai/code-agent:0.1.0",
		"allowPublicRegistration": false,
	})
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/settings", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "rp-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(published) != 1 || published[0] != "ghcr.io/roundpenai/code-agent:0.1.0" {
		t.Fatalf("published = %v", published)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/admin/settings", nil)
	req.Header.Set("X-API-Key", "rp-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var envelope struct {
		Settings settings.AppSettings `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if envelope.Settings.AgentImage != "ghcr.io/roundpenai/code-agent:0.1.0" {
		t.Fatalf("GET agentImage = %q", envelope.Settings.AgentImage)
	}
}
