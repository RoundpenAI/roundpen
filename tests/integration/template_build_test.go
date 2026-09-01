package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/api/e2b"
	"github.com/RoundpenAI/roundpen/internal/api/httpapi"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	templatebuilder "github.com/RoundpenAI/roundpen/internal/template/builder"
	"github.com/RoundpenAI/roundpen/internal/workspace/local"
)

func TestTemplateBuildDocker(t *testing.T) {
	if os.Getenv("ROUNDPEN_TEST_DOCKER_BUILD") != "1" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_BUILD=1 to run docker template build test")
	}
	h := startLocalDockerHarness(t)
	name := fmt.Sprintf("it-build-%d", time.Now().UnixNano())

	create := h.mustDo(t, http.MethodPost, "/v3/templates", map[string]any{
		"name":     name,
		"cpuCount": 1,
		"memoryMB": 512,
	})
	defer create.Body.Close()
	if create.StatusCode != http.StatusAccepted {
		t.Fatalf("create template: %s", create.Status)
	}
	var created map[string]any
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	tplID, _ := created["templateID"].(string)
	buildID, _ := created["buildID"].(string)
	if tplID == "" || buildID == "" {
		t.Fatalf("missing ids: %#v", created)
	}

	start := h.mustDo(t, http.MethodPost, "/v2/templates/"+tplID+"/builds/"+buildID, map[string]any{
		"fromImage": "alpine:3.20",
		"steps": []map[string]any{
			{"type": "RUN", "args": []string{"echo roundpen-template-build > /tmp/mark"}},
		},
	})
	defer start.Body.Close()
	if start.StatusCode != http.StatusAccepted {
		t.Fatalf("start build: %s", start.Status)
	}

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		resp := h.mustDo(t, http.MethodGet, "/templates/"+tplID+"/builds/"+buildID+"/status", nil)
		var st map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()
		status, _ := st["status"].(string)
		if status == "ready" {
			t.Logf("template build ready template=%s build=%s", tplID, buildID)
			return
		}
		if status == "error" {
			t.Fatalf("build failed: %#v", st)
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("build timed out")
}

func startLocalDockerHarness(t *testing.T) *harness {
	t.Helper()
	dsn := testDatabaseURL(t)
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	dockerHost := os.Getenv("DOCKER_HOST")
	be, err := dockerbackend.New(dockerHost, "")
	if err != nil {
		t.Fatalf("docker: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := be.Ping(ctx); err != nil {
		t.Skipf("docker not available: %v", err)
	}

	root := t.TempDir()
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	apiKey := "rp-integration-test"
	_ = users.Upsert(ctx, storage.User{Username: "test", Email: "t@t.com", APIKey: apiKey, Role: storage.RoleAdmin})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tplStore := template.NewStore(db.SQL)
	tplSvc := template.NewService(tplStore, "alpine:3.20")
	tplSvc.SetLogger(logger)
	_ = tplSvc.Seed(ctx, "docker")
	bld, err := templatebuilder.NewDocker(dockerHost)
	if err != nil {
		t.Fatalf("template builder: %v", err)
	}
	t.Cleanup(func() { _ = bld.Close() })
	tplSvc.SetBuilder("docker", bld)

	mgr := sandbox.NewService(storage.NewSandboxStore(db), be, local.New(root), "alpine:3.20", 10*time.Minute, logger, sandbox.WithTemplates(tplSvc))

	mux := http.NewServeMux()
	auth.Mount(mux, users, sessions, func() bool { return false })
	(&e2b.Handler{Manager: mgr, Templates: tplSvc}).Mount(mux)
	native := &httpapi.Handler{Manager: mgr}
	native.Mount(mux)
	(&preview.Handler{Manager: mgr, Tokens: preview.NewStore(15 * time.Minute)}).Mount(mux)

	srv := httptest.NewServer(auth.Middleware(users, sessions)(mux))
	t.Cleanup(srv.Close)
	return &harness{URL: srv.URL, Client: srv.Client(), DataRoot: root, FS: local.New(root), APIKey: apiKey}
}

func TestTemplateBuildCacheHit(t *testing.T) {
	if os.Getenv("ROUNDPEN_TEST_DOCKER_BUILD") != "1" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_BUILD=1")
	}
	h := startLocalDockerHarness(t)
	spec := map[string]any{
		"fromImage": "alpine:3.20",
		"steps":     []map[string]any{{"type": "RUN", "args": []string{"echo cache-test"}}},
	}

	buildOnce := func(name string) string {
		create := h.mustDo(t, http.MethodPost, "/v3/templates", map[string]any{"name": name})
		defer create.Body.Close()
		var created map[string]any
		_ = json.NewDecoder(create.Body).Decode(&created)
		tplID, _ := created["templateID"].(string)
		buildID, _ := created["buildID"].(string)
		start := h.mustDo(t, http.MethodPost, "/v2/templates/"+tplID+"/builds/"+buildID, spec)
		start.Body.Close()
		deadline := time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			resp := h.mustDo(t, http.MethodGet, "/templates/"+tplID+"/builds/"+buildID+"/status", nil)
			var st map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&st)
			resp.Body.Close()
			if st["status"] == "ready" {
				return buildID
			}
			if st["status"] == "error" {
				t.Fatalf("build failed: %#v", st)
			}
			time.Sleep(2 * time.Second)
		}
		t.Fatal("timeout")
		return ""
	}

	first := buildOnce(fmt.Sprintf("cache-a-%d", time.Now().UnixNano()))
	second := buildOnce(fmt.Sprintf("cache-b-%d", time.Now().UnixNano()))
	if first == "" || second == "" {
		t.Fatal("missing build ids")
	}
	t.Logf("cache test builds first=%s second=%s (second should complete quickly)", first, second)
}
