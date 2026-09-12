package template

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestStore_GetUpdateDeleteTemplate(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	name := "crud-" + uuid.NewString()[:8]
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })

	created, err := store.CreateTemplate(ctx, CreateTemplateRequest{
		Name: name, CPUCount: 2, MemoryMB: 1024, Public: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.GetByID(ctx, created.TemplateID)
	if err != nil || got.Name != name {
		t.Fatalf("GetByID: err=%v got=%+v", err, got)
	}

	desc := "updated description"
	pub := false
	cpu := 4
	profile := "browser"
	if err := store.UpdateTemplate(ctx, created.TemplateID, UpdateTemplateRequest{
		Description: &desc,
		Public:      &pub,
		CPUCount:    &cpu,
		Profile:     &profile,
	}); err != nil {
		t.Fatal(err)
	}

	got, err = store.GetByID(ctx, created.TemplateID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != desc || got.Public || got.CPUCount != 4 || got.Profile != "browser" {
		t.Fatalf("after update: %+v", got)
	}

	builds, err := store.ListBuilds(ctx, created.TemplateID)
	if err != nil || len(builds) == 0 {
		t.Fatalf("ListBuilds: err=%v len=%d", err, len(builds))
	}

	if err := store.DeleteTemplate(ctx, created.TemplateID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetByID(ctx, created.TemplateID); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestStore_UpdateBuiltinAllowedDeleteRejected(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	rec, err := store.GetByID(ctx, mustTemplateIDByName(t, store, "code-agent"))
	if err != nil {
		t.Fatal(err)
	}
	desc := "admin-edited builtin"
	if err := store.UpdateTemplate(ctx, rec.TemplateID, UpdateTemplateRequest{Description: &desc}); err != nil {
		t.Fatalf("builtin update should be allowed: %v", err)
	}
	got, err := store.GetByID(ctx, rec.TemplateID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != desc {
		t.Fatalf("description=%q", got.Description)
	}
	if err := store.DeleteTemplate(ctx, rec.TemplateID); err == nil {
		t.Fatal("expected builtin delete error")
	}
}

func mustTemplateIDByName(t *testing.T, store *Store, name string) string {
	t.Helper()
	list, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range list {
		if rec.Name == name {
			return rec.TemplateID
		}
	}
	t.Fatalf("template %q not found", name)
	return ""
}
