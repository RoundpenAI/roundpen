package template

import (
	"context"
	"testing"
)

func TestStore_SeedBuiltin_idempotent(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
			t.Fatalf("seed run %d: %v", i+1, err)
		}
	}

	var count int
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT count(*) FROM templates
		WHERE namespace=$1 AND name IN ('base','python','node','code-agent','browser-desktop')`, DefaultNamespace).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("template count=%d want 5", count)
	}
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT count(*) FROM template_builds b
		JOIN template_tags tg ON tg.build_id=b.id AND tg.tag='default'
		JOIN templates t ON t.id=tg.template_id
		WHERE t.namespace=$1 AND t.name IN ('base','python','node','code-agent','browser-desktop')`, DefaultNamespace).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("default build count=%d want 5", count)
	}
}

func TestStore_SeedBuiltin_repairsMissingTag(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	var tplID string
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT id FROM templates WHERE namespace=$1 AND name='base'`, DefaultNamespace).Scan(&tplID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM template_tags WHERE template_id=$1`, tplID); err != nil {
		t.Fatal(err)
	}

	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveByTag(ctx, ParsedRef{Namespace: DefaultNamespace, Name: "base", Tag: DefaultTag}); err != nil {
		t.Fatalf("resolve after repair: %v", err)
	}
}

func TestStore_SeedBuiltin_repairsBrokenBuild(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	var tplID, buildID string
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT t.id, tg.build_id
		FROM templates t
		JOIN template_tags tg ON tg.template_id=t.id AND tg.tag='default'
		WHERE t.namespace=$1 AND t.name='python'`, DefaultNamespace).Scan(&tplID, &buildID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		UPDATE template_builds SET status='waiting', artifact_ref='', error_message='broken' WHERE id=$1`, buildID); err != nil {
		t.Fatal(err)
	}

	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	res, err := store.ResolveByTag(ctx, ParsedRef{Namespace: DefaultNamespace, Name: "python", Tag: DefaultTag})
	if err != nil {
		t.Fatal(err)
	}
	if res.Image != "python:3.12-slim" {
		t.Fatalf("image=%q", res.Image)
	}
	var status string
	if err := sqlDB.QueryRowContext(ctx, `SELECT status FROM template_builds WHERE id=$1`, buildID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ready" {
		t.Fatalf("status=%q", status)
	}
}

func TestStore_SeedBuiltin_codeAgentUsesAgentImage(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()

	t.Setenv("ROUNDPEN_AGENT_IMAGE", "ghcr.io/example/code-agent:9.9.9")
	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	res, err := store.ResolveByTag(ctx, ParsedRef{Namespace: DefaultNamespace, Name: "code-agent", Tag: DefaultTag})
	if err != nil {
		t.Fatal(err)
	}
	if res.Image != "ghcr.io/example/code-agent:9.9.9" {
		t.Fatalf("code-agent image=%q", res.Image)
	}
}
