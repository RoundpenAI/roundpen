package template

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/template/builder"
)

type mockBuilder struct {
	mu       sync.Mutex
	calls    int
	artifact string
	snapshot bool
	err      error
}

func (m *mockBuilder) Build(_ context.Context, _ string, _ builder.Spec, _ []string, log builder.LogFn) (string, bool, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if log != nil {
		log("info", "mock", "build complete")
	}
	if m.err != nil {
		return "", false, m.err
	}
	return m.artifact, m.snapshot, nil
}

func TestService_StartBuild_requiresBuilder(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()
	svc := NewService(store, "host")

	name := fmt.Sprintf("nobuild-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })
	created, err := svc.CreateTemplate(ctx, CreateTemplateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.StartBuild(ctx, created.TemplateID, created.BuildID, BuildSpec{FromImage: "alpine:3.20"}, CreateBuildRequest{})
	if err == nil || err.Error() != "template builds are not configured (set ROUNDPEN_TEMPLATE_BUILDER=docker|kaniko)" {
		t.Fatalf("StartBuild err=%v", err)
	}
}

func TestService_StartBuild_asyncWithMockBuilder(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()
	svc := NewService(store, "host")
	svc.SetBuilder("docker", &mockBuilder{artifact: "roundpen/test:1", snapshot: true})

	name := fmt.Sprintf("mockbuild-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })
	created, err := svc.CreateTemplate(ctx, CreateTemplateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}

	spec := BuildSpec{
		FromImage: "alpine:3.20",
		StartCmd:  "sleep 1",
		ReadyCmd:  "waitForTimeout(100)",
	}
	if _, err := svc.StartBuild(ctx, created.TemplateID, created.BuildID, spec, CreateBuildRequest{}); err != nil {
		t.Fatal(err)
	}

	waitBuildReady(t, svc, created.TemplateID, created.BuildID)

	info, logs, err := svc.GetBuildStatus(ctx, created.TemplateID, created.BuildID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != BuildReady || info.ArtifactRef != "roundpen/test:1" || !info.Snapshot {
		t.Fatalf("info=%+v", info)
	}
	if len(logs) == 0 {
		t.Fatal("expected build logs")
	}

	res, err := svc.Resolve(ctx, name)
	if err != nil || !res.UseImageCmd || res.StartCmd != "sleep 1" {
		t.Fatalf("resolve after build: err=%v res=%+v", err, res)
	}
}

func TestService_StartBuild_cacheHit(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()
	mb := &mockBuilder{artifact: "should-not-be-called"}
	svc := NewService(store, "host")
	svc.SetBuilder("docker", mb)

	name := fmt.Sprintf("cachehit-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })
	created, err := svc.CreateTemplate(ctx, CreateTemplateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}

	spec := BuildSpec{FromImage: "alpine:3.20", Steps: []Step{{Type: "RUN", Args: []string{"true"}}}}
	key, err := builder.CacheKey(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishBuild(ctx, created.TemplateID, created.BuildID, "roundpen/cached:old", key, true, true); err != nil {
		t.Fatal(err)
	}

	build2ID := uuid.NewString()
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO template_builds (id, template_id, status, artifact_ref, cpu_count, memory_mb, disk_size_mb, envd_version)
		VALUES ($1,$2,'waiting','',1,512,5120,$3)`,
		build2ID, created.TemplateID, EnvdVersion)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.StartBuild(ctx, created.TemplateID, build2ID, spec, CreateBuildRequest{}); err != nil {
		t.Fatal(err)
	}
	info, _, err := svc.GetBuildStatus(ctx, created.TemplateID, build2ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != BuildReady || info.ArtifactRef != "roundpen/cached:old" {
		t.Fatalf("cache hit info=%+v", info)
	}
	mb.mu.Lock()
	calls := mb.calls
	mb.mu.Unlock()
	if calls != 0 {
		t.Fatalf("builder should not run on cache hit, calls=%d", calls)
	}
}

func TestService_StartBuild_fromTemplateBase(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()
	if err := store.SeedBuiltin(ctx, "kern", "host"); err != nil {
		t.Fatal(err)
	}

	mb := &mockBuilder{artifact: "roundpen/fromtpl:1"}
	svc := NewService(store, "host")
	svc.SetBuilder("docker", mb)

	name := fmt.Sprintf("fromtpl-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })
	created, err := svc.CreateTemplate(ctx, CreateTemplateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	spec := BuildSpec{FromTemplate: "python", Steps: []Step{{Type: "RUN", Args: []string{"true"}}}}
	if _, err := svc.StartBuild(ctx, created.TemplateID, created.BuildID, spec, CreateBuildRequest{}); err != nil {
		t.Fatal(err)
	}
	waitBuildReady(t, svc, created.TemplateID, created.BuildID)
	if mb.calls != 1 {
		t.Fatalf("builder calls=%d", mb.calls)
	}
}

func TestService_StartBuild_reuseReadySameSpec(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()
	mb := &mockBuilder{artifact: "roundpen/reuse:1"}
	svc := NewService(store, "host")
	svc.SetBuilder("docker", mb)

	name := fmt.Sprintf("reuse-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })
	created, err := svc.CreateTemplate(ctx, CreateTemplateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	spec := BuildSpec{FromImage: "alpine:3.20", Steps: []Step{{Type: "RUN", Args: []string{"true"}}}}
	out, err := svc.StartBuild(ctx, created.TemplateID, created.BuildID, spec, CreateBuildRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Forked || out.BuildID != created.BuildID {
		t.Fatalf("first start: %+v", out)
	}
	waitBuildReady(t, svc, created.TemplateID, created.BuildID)

	mb.artifact = "roundpen/reuse:2"
	out, err = svc.StartBuild(ctx, created.TemplateID, created.BuildID, spec, CreateBuildRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Forked || out.BuildID != created.BuildID {
		t.Fatalf("same-spec rebuild should reuse buildID, got %+v", out)
	}
	waitBuildReady(t, svc, created.TemplateID, created.BuildID)
	if mb.calls != 2 {
		t.Fatalf("builder calls=%d", mb.calls)
	}
}

func TestService_StartBuild_forkReadyOnSpecChange(t *testing.T) {
	store, sqlDB := testStore(t)
	ctx := context.Background()
	mb := &mockBuilder{artifact: "roundpen/fork:1"}
	svc := NewService(store, "host")
	svc.SetBuilder("docker", mb)

	name := fmt.Sprintf("fork-%s", uuid.NewString()[:8])
	t.Cleanup(func() { deleteTemplateByName(t, sqlDB, DefaultNamespace, name) })
	created, err := svc.CreateTemplate(ctx, CreateTemplateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	spec1 := BuildSpec{FromImage: "alpine:3.20"}
	if _, err := svc.StartBuild(ctx, created.TemplateID, created.BuildID, spec1, CreateBuildRequest{}); err != nil {
		t.Fatal(err)
	}
	waitBuildReady(t, svc, created.TemplateID, created.BuildID)

	mb.artifact = "roundpen/fork:2"
	spec2 := BuildSpec{FromImage: "alpine:3.21"}
	assign := true
	out, err := svc.StartBuild(ctx, created.TemplateID, created.BuildID, spec2, CreateBuildRequest{
		Tags:          []string{"v2"},
		AssignDefault: &assign,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Forked || out.BuildID == created.BuildID {
		t.Fatalf("expected forked build, got %+v", out)
	}
	waitBuildReady(t, svc, created.TemplateID, out.BuildID)

	old, err := store.GetBuild(ctx, created.TemplateID, created.BuildID)
	if err != nil || old.Status != BuildReady {
		t.Fatalf("old build should stay ready: %+v err=%v", old, err)
	}
	tags, err := store.ListTags(ctx, created.TemplateID)
	if err != nil {
		t.Fatal(err)
	}
	foundV2 := false
	for _, tg := range tags {
		if tg.Tag == "v2" && tg.BuildID == out.BuildID {
			foundV2 = true
		}
		if tg.Tag == DefaultTag && tg.BuildID != out.BuildID {
			t.Fatalf("default should move to forked build, tags=%+v", tags)
		}
	}
	if !foundV2 {
		t.Fatalf("missing v2 tag, tags=%+v", tags)
	}
}

func waitBuildReady(t *testing.T, svc *Service, templateID, buildID string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, _, err := svc.GetBuildStatus(ctx, templateID, buildID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		switch info.Status {
		case BuildReady:
			return
		case BuildError:
			t.Fatalf("build error: %s", info.ErrorMessage)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("build timed out")
}
