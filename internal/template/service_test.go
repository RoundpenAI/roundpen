package template

import (
	"context"
	"testing"
)

func TestService_Resolve_registeredAndLegacy(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	svc := NewService(store, "ghcr.io/roundpenai/code-agent:0.1.0")
	if err := svc.Seed(ctx, "docker"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Resolve(ctx, "base")
	if err != nil || res.Image != "ubuntu:22.04" {
		t.Fatalf("base resolve: err=%v res=%+v", err, res)
	}

	res, err = svc.Resolve(ctx, "python")
	if err != nil || res.MemoryMB != 1024 {
		t.Fatalf("python resolve: err=%v res=%+v", err, res)
	}

	// A tag suffix still finds the catalog entry.
	res, err = svc.Resolve(ctx, "default/python:1.0")
	if err != nil || res.Image != "python:3.12-slim" {
		t.Fatalf("tagged resolve: err=%v res=%+v", err, res)
	}

	res, err = svc.Resolve(ctx, "alpine:3.20")
	if err != nil || res.Image != "alpine:3.20" {
		t.Fatalf("legacy resolve: err=%v res=%+v", err, res)
	}
}

func TestService_List(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	svc := NewService(store, "ghcr.io/roundpenai/code-agent:0.1.0")
	if err := svc.Seed(ctx, "docker"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) < 5 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}
}
