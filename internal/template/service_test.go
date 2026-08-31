package template

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestService_Resolve_registeredAndLegacy(t *testing.T) {
	store, _, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()
	svc := NewService(store, "host")
	if err := svc.Seed(ctx, "kern"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Resolve(ctx, "host")
	if err != nil || res.Image != "host" {
		t.Fatalf("host resolve: err=%v res=%+v", err, res)
	}

	res, err = svc.Resolve(ctx, "python")
	if err != nil || res.MemoryMB != 1024 {
		t.Fatalf("python resolve: err=%v res=%+v", err, res)
	}

	res, err = svc.Resolve(ctx, "alpine:3.20")
	if err != nil || res.Image != "alpine:3.20" {
		t.Fatalf("legacy resolve: err=%v res=%+v", err, res)
	}
}

func TestService_Exists(t *testing.T) {
	store, _, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()
	svc := NewService(store, "host")
	if err := svc.Seed(ctx, "kern"); err != nil {
		t.Fatal(err)
	}
	ok, err := svc.Exists(ctx, "python")
	if err != nil || !ok {
		t.Fatalf("Exists python: ok=%v err=%v", ok, err)
	}
	ok, err = svc.Exists(ctx, "no-such-template")
	if err != nil || ok {
		t.Fatalf("Exists missing: ok=%v err=%v", ok, err)
	}
}

func TestService_List(t *testing.T) {
	store, _, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()
	svc := NewService(store, "host")
	if err := svc.Seed(ctx, "kern"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) < 5 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}
}

func TestService_ResolveByBuildID(t *testing.T) {
	store, sqlDB, cleanup := testStore(t)
	defer cleanup()
	ctx := context.Background()
	svc := NewService(store, "host")

	created, err := store.CreateTemplate(ctx, CreateTemplateRequest{Name: "resolve-build-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, created.Name) })
	if err := store.FinishBuild(ctx, created.TemplateID, created.BuildID, "node:22-bookworm", "k", true, true); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Resolve(ctx, created.BuildID)
	if err != nil || res.Image != "node:22-bookworm" || !res.UseImageCmd {
		t.Fatalf("resolve by build id: err=%v res=%+v", err, res)
	}
}

func TestService_BuildsSupported(t *testing.T) {
	svc := NewService(nil, "host")
	if svc.BuildsSupported() {
		t.Fatal("expected false without builder")
	}
}
