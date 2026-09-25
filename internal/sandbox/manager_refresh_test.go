package sandbox_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/workspace/local"
)

func TestService_RefreshTemplateImagePassthrough(t *testing.T) {
	be := newStubBackend("docker")
	be.refreshChanged = true
	be.refreshDigest = "sha256:abc"
	svc, _, _ := newTestService(t, be)

	image, changed, digest, err := svc.RefreshTemplateImage(adminCtx(), "python:3.12-slim")
	if err != nil {
		t.Fatal(err)
	}
	if image != "python:3.12-slim" || !changed || digest != "sha256:abc" {
		t.Fatalf("image=%q changed=%v digest=%q", image, changed, digest)
	}
	be.mu.Lock()
	ref := be.refreshRef
	be.mu.Unlock()
	if ref != "python:3.12-slim" {
		t.Fatalf("backend ref=%q", ref)
	}
}

func TestService_RefreshImagePassthrough(t *testing.T) {
	be := newStubBackend("docker")
	be.refreshChanged = true
	be.refreshDigest = "sha256:def"
	svc, _, _ := newTestService(t, be)

	image, changed, digest, err := svc.RefreshImage(adminCtx(), "  ghcr.io/team/agent:manual  ")
	if err != nil {
		t.Fatal(err)
	}
	if image != "ghcr.io/team/agent:manual" || !changed || digest != "sha256:def" {
		t.Fatalf("image=%q changed=%v digest=%q", image, changed, digest)
	}
	be.mu.Lock()
	ref := be.refreshRef
	be.mu.Unlock()
	if ref != "ghcr.io/team/agent:manual" {
		t.Fatalf("backend ref=%q", ref)
	}
}

func TestService_RefreshImageRejectsEmptyRef(t *testing.T) {
	svc, _, _ := newTestService(t, newStubBackend("docker"))
	if _, _, _, err := svc.RefreshImage(adminCtx(), "   "); err == nil {
		t.Fatal("expected an error for an empty ref")
	}
}

func TestService_RefreshTemplateImageBackendError(t *testing.T) {
	be := newStubBackend("docker")
	be.refreshErr = errors.New("daemon unreachable")
	svc, _, _ := newTestService(t, be)

	image, _, _, err := svc.RefreshTemplateImage(adminCtx(), "python:3.12-slim")
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if image != "python:3.12-slim" {
		t.Fatalf("image=%q", image)
	}
}

func TestService_RefreshTemplateImageResolvesTemplate(t *testing.T) {
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := adminCtx()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tplSvc := template.NewService(template.NewStore(db.SQL), "host")
	if err := tplSvc.Seed(ctx, "stub"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	be := newStubBackend("stub")
	svc := sandbox.NewService(newMemStore(), be, local.New(t.TempDir()), "host", time.Minute, nil, sandbox.WithTemplates(tplSvc))

	image, _, _, err := svc.RefreshTemplateImage(ctx, "python")
	if err != nil {
		t.Fatal(err)
	}
	if image != "python:3.12-slim" {
		t.Fatalf("resolved image=%q", image)
	}
	be.mu.Lock()
	ref := be.refreshRef
	be.mu.Unlock()
	if ref != "python:3.12-slim" {
		t.Fatalf("backend ref=%q", ref)
	}
}
