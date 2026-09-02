package integration_test

import (
	"context"
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
	"github.com/RoundpenAI/roundpen/internal/backend"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
	kernbackend "github.com/RoundpenAI/roundpen/internal/backend/kern"
	"github.com/RoundpenAI/roundpen/internal/preview"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/workspace"
	"github.com/RoundpenAI/roundpen/internal/workspace/local"
	"github.com/RoundpenAI/roundpen/internal/workspace/sshfs"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("ROUNDPEN_TEST_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL for integration tests")
	return ""
}

type harness struct {
	URL      string
	Client   *http.Client
	DataRoot string
	FS       workspace.FS
	APIKey   string
}

func startHarness(t *testing.T, be backend.Backend, fs workspace.FS, dataRoot, defaultImage string) *harness {
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

	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	apiKey := "rp-integration-test"
	if err := users.Upsert(ctx, storage.User{
		Username: "test",
		Email:    "test@example.com",
		APIKey:   apiKey,
		Role:     storage.RoleAdmin,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tplStore := template.NewStore(db.SQL)
	tplSvc := template.NewService(tplStore, defaultImage)
	if err := tplSvc.Seed(ctx, be.Name()); err != nil {
		t.Fatalf("template seed: %v", err)
	}
	mgr := sandbox.NewService(storage.NewSandboxStore(db), be, fs, defaultImage, 10*time.Minute, logger, sandbox.WithTemplates(tplSvc))

	mux := http.NewServeMux()
	auth.Mount(mux, users, sessions, func() bool { return false })
	(&e2b.Handler{Manager: mgr, Templates: tplSvc}).Mount(mux)
	native := &httpapi.Handler{Manager: mgr}
	native.Mount(mux)
	native.MountTerminal(mux)
	(&preview.Handler{
		Manager: mgr,
		Tokens:  preview.NewStore(15 * time.Minute),
	}).Mount(mux)

	srv := httptest.NewServer(auth.Middleware(users, sessions)(mux))
	t.Cleanup(srv.Close)

	return &harness{URL: srv.URL, Client: srv.Client(), DataRoot: dataRoot, FS: fs, APIKey: apiKey}
}

func startKernHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	return startHarness(t, kernbackend.New(), local.New(root), root, "host")
}

func startDockerSSHHarness(t *testing.T) *harness {
	t.Helper()
	host := os.Getenv("ROUNDPEN_TEST_DOCKER_HOST")
	if host == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_HOST=ssh://user@host")
	}
	root := os.Getenv("ROUNDPEN_TEST_REMOTE_ROOT")
	if root == "" {
		root = "/tmp/roundpen-it"
	}

	be, err := dockerbackend.New(host, "")
	if err != nil {
		t.Fatalf("docker backend: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := be.Ping(ctx); err != nil {
		t.Fatalf("docker ping: %v", err)
	}

	fs, err := sshfs.NewFromDockerHost(host, root)
	if err != nil {
		t.Fatalf("sshfs: %v", err)
	}

	image := os.Getenv("ROUNDPEN_TEST_DOCKER_IMAGE")
	if image == "" {
		image = "alpine:3.20"
	}
	return startHarness(t, be, fs, root, image)
}
