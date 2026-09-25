package template

import (
	"context"
	"errors"
	"testing"
)

func TestStore_SeedAndResolve(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()

	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	res, err := store.ResolveByName(ctx, ParsedRef{Namespace: DefaultNamespace, Name: "base"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Image != "ubuntu:22.04" {
		t.Fatalf("image=%q", res.Image)
	}
	if res.CPUCount != 1 || res.MemoryMB != 512 {
		t.Fatalf("resources cpu=%d mem=%d", res.CPUCount, res.MemoryMB)
	}
}

func TestStore_ResolveUnknown(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()

	_, err := store.ResolveByName(ctx, ParsedRef{Namespace: DefaultNamespace, Name: "no-such-template"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestStore_ListAndRecordSpawn(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 5 {
		t.Fatalf("expected seeded templates, got %d", len(list))
	}
	for _, rec := range list {
		if rec.BuildStatus != BuildReady {
			t.Fatalf("template %s buildStatus=%q want ready", rec.Name, rec.BuildStatus)
		}
	}
	if err := store.RecordSpawn(ctx, list[0].TemplateID); err != nil {
		t.Fatal(err)
	}
}
