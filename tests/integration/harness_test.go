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
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/workspace"
	"github.com/RoundpenAI/roundpen/internal/workspace/local"
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := sandbox.NewService(storage.NewSandboxStore(db), be, fs, defaultImage, 10*time.Minute, logger)

	mux := http.NewServeMux()
	(&e2b.Handler{Manager: mgr}).Mount(mux)
	(&httpapi.Handler{Manager: mgr}).Mount(mux)

	srv := httptest.NewServer(auth.APIKey("", mux))
	t.Cleanup(srv.Close)

	return &harness{URL: srv.URL, Client: srv.Client(), DataRoot: dataRoot}
}

func startKernHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	return startHarness(t, kernbackend.New(), local.New(root), root, "host")
}

func startDockerSSHHarness(t *testing.T) *harness {
	t.Helper()
	host := os.Getenv("ROUNDPEN_TEST_DOCKER_HOST")
	mount := os.Getenv("ROUNDPEN_TEST_REMOTE_MOUNT")
	if host == "" || mount == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_HOST and ROUNDPEN_TEST_REMOTE_MOUNT")
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

	image := os.Getenv("ROUNDPEN_TEST_DOCKER_IMAGE")
	if image == "" {
		image = "alpine:3.20"
	}
	return startHarness(t, be, &fixedRemoteFS{path: mount}, mount, image)
}

// fixedRemoteFS always returns the same host path (must exist on the Docker host).
type fixedRemoteFS struct {
	path string
}

func (f *fixedRemoteFS) Create(ctx context.Context, id string, ephemeral bool) (*workspace.Info, error) {
	_ = ctx
	return &workspace.Info{ID: id, HostPath: f.path, Ephemeral: ephemeral}, nil
}

func (f *fixedRemoteFS) Get(ctx context.Context, id string) (*workspace.Info, error) {
	return f.Create(ctx, id, false)
}

func (f *fixedRemoteFS) Remove(ctx context.Context, id string) error { return nil }

func (f *fixedRemoteFS) Open(ctx context.Context, id, relPath string) (io.ReadCloser, error) {
	return nil, os.ErrInvalid
}

func (f *fixedRemoteFS) Write(ctx context.Context, id, relPath string, r io.Reader) error {
	return os.ErrInvalid
}

func (f *fixedRemoteFS) Stat(ctx context.Context, id, relPath string) (os.FileInfo, error) {
	return nil, os.ErrInvalid
}

var _ workspace.FS = (*fixedRemoteFS)(nil)
