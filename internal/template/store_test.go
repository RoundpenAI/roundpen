package template

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

func TestStore_SeedAndResolveByTag(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	if err := store.SeedBuiltin(ctx, "kern", "host"); err != nil {
		t.Fatal(err)
	}
	res, err := store.ResolveByTag(ctx, ParsedRef{Namespace: DefaultNamespace, Name: "host", Tag: DefaultTag})
	if err != nil {
		t.Fatal(err)
	}
	if res.Image != "host" {
		t.Fatalf("image=%q", res.Image)
	}
	if res.CPUCount != 1 || res.MemoryMB != 512 {
		t.Fatalf("resources cpu=%d mem=%d", res.CPUCount, res.MemoryMB)
	}
	_ = sqlDB
}

func TestStore_ResolveByTag_withStagingTag(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	name := fmt.Sprintf("tagged-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })

	created, err := store.CreateTemplate(ctx, CreateTemplateRequest{
		Name: name, Tag: "staging", CPUCount: 2, MemoryMB: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := BuildSpec{FromImage: "alpine:3.20", Steps: []Step{{Type: "RUN", Args: []string{"true"}}}}
	key, err := builder.CacheKey(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBuildSpec(ctx, created.BuildID, spec, key); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishBuild(ctx, created.TemplateID, created.BuildID, "alpine:3.20", key, false, false); err != nil {
		t.Fatal(err)
	}

	res, err := store.ResolveByTag(ctx, ParsedRef{Namespace: DefaultNamespace, Name: name, Tag: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	if res.CPUCount != 2 || res.MemoryMB != 2048 {
		t.Fatalf("resources cpu=%d mem=%d", res.CPUCount, res.MemoryMB)
	}
}

func TestStore_ResolveByBuildID(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	name := fmt.Sprintf("bybuild-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })

	created, err := store.CreateTemplate(ctx, CreateTemplateRequest{Name: name, CPUCount: 1, MemoryMB: 512})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishBuild(ctx, created.TemplateID, created.BuildID, "python:3.12-slim", "cache1", true, true); err != nil {
		t.Fatal(err)
	}

	res, err := store.ResolveByBuildID(ctx, created.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Snapshot || !res.UseImageCmd || res.Image != "python:3.12-slim" {
		t.Fatalf("resolved=%+v", res)
	}
}

func TestStore_ListAndRecordSpawn(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	if err := store.SeedBuiltin(ctx, "kern", "host"); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 5 {
		t.Fatalf("expected seeded templates, got %d", len(list))
	}
	if err := store.RecordSpawn(ctx, list[0].TemplateID); err != nil {
		t.Fatal(err)
	}
}

func TestStore_CreateTemplate_duplicateName(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	name := fmt.Sprintf("dup-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })

	if _, err := store.CreateTemplate(ctx, CreateTemplateRequest{Name: name}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTemplate(ctx, CreateTemplateRequest{Name: name}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestStore_FindCachedBuild(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	name := fmt.Sprintf("cache-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })

	created, err := store.CreateTemplate(ctx, CreateTemplateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	spec := BuildSpec{FromImage: "alpine:3.20"}
	key, err := builder.CacheKey(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishBuild(ctx, created.TemplateID, created.BuildID, "roundpen/cached:1", key, false, true); err != nil {
		t.Fatal(err)
	}

	cached, err := store.FindCachedBuild(ctx, created.TemplateID, key)
	if err != nil {
		t.Fatal(err)
	}
	if cached.ArtifactRef != "roundpen/cached:1" {
		t.Fatalf("artifact=%q", cached.ArtifactRef)
	}
}

func TestStore_BuildLogs(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	name := fmt.Sprintf("logs-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })

	created, err := store.CreateTemplate(ctx, CreateTemplateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendBuildLog(ctx, created.BuildID, "info", "build", "hello"); err != nil {
		t.Fatal(err)
	}
	logs, err := store.ListBuildLogs(ctx, created.BuildID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Message != "hello" {
		t.Fatalf("logs=%+v", logs)
	}
	info, err := store.GetBuild(ctx, created.TemplateID, created.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != BuildWaiting {
		t.Fatalf("status=%q", info.Status)
	}
}
