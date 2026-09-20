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
		WHERE namespace=$1 AND name IN ('base','python','node','code-agent','browser')`, DefaultNamespace).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("template count=%d want 5", count)
	}
	if err := sqlDB.QueryRowContext(ctx, `
		SELECT count(*) FROM templates
		WHERE namespace=$1 AND name IN ('base','python','node','code-agent','browser')
		  AND artifact_ref=''`, DefaultNamespace).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("seeded templates without an artifact=%d want 0", count)
	}
}

func TestStore_SeedBuiltin_restoresArtifact(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()

	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		UPDATE templates SET artifact_ref='' WHERE namespace=$1 AND name='base'`, DefaultNamespace); err != nil {
		t.Fatal(err)
	}

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
}

func TestStore_SeedBuiltin_codeAgentUsesAgentImage(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()

	t.Setenv("ROUNDPEN_AGENT_IMAGE", "ghcr.io/example/code-agent:9.9.9")
	if err := store.SeedBuiltin(ctx, "docker", "ghcr.io/roundpenai/code-agent:0.1.0"); err != nil {
		t.Fatal(err)
	}
	res, err := store.ResolveByName(ctx, ParsedRef{Namespace: DefaultNamespace, Name: "code-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Image != "ghcr.io/example/code-agent:9.9.9" {
		t.Fatalf("code-agent image=%q", res.Image)
	}
}
