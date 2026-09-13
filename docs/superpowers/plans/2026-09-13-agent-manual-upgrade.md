# Agent 环境手动升级 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给用户一个自助入口，把正在运行的 Agent 环境升级到当前模板配置的镜像（先检查 digest、变了才重建，支持 force 强制重建）。

**Architecture:** 新增引擎级原语 `Backend.RefreshImage`（拉取 + digest 比对），经 `sandbox.Manager.RefreshTemplateImage`（模板解析在 manager 侧）暴露给 `userenv.UpgradeAgent`；重建复用现有"Delete → EnsureAgent"路径（稳定 id 复活、持久 workspace 保留、env 重新组装全部复用）。HTTP 层加 `POST /v1/me/environments/agent/upgrade`，设置页新增「Agent 环境」区块。

**Tech Stack:** Go（net/http、io.Pipe 测试）、Docker SDK v25、PostgreSQL、React + Semi UI（@douyinfe/semi-ui-19）、i18n（en/zh_CN）。

**Spec:** `docs/superpowers/specs/2026-09-13-agent-manual-upgrade-design.md`

**分支:** 在 `feat/agent-upgrade` 上执行（设计文档已提交于 `1348708`）。

**前置命令（每个任务开始前确保）：**

```bash
git branch --show-current   # 期望 feat/agent-upgrade
```

---

### Task 0: 前置依赖（稳定 id 重建链）

**Files:** 无（仅 git 操作）

`UpgradeAgent` 依赖 `fix/agent-container-user` 中的存储复活（软删除行同 id 重建）与容器属主身份。

- [ ] **Step 1: 合并依赖分支**

若 master 已含 `fix/agent-container-user` 的合并提交，执行：

```bash
git merge master
```

否则：

```bash
git merge fix/agent-container-user
```

- [ ] **Step 2: 验证**

Run: `go build ./... && go test ./internal/sandbox/ ./internal/storage/ ./internal/backend/... -count=1`
Expected: 全部 ok（storage 的 `TestPgSandboxStore_RecreateAfterSoftDelete` 无 env 时 skip 属正常）

- [ ] **Step 3: 提交（如有冲突解决）**

```bash
git status --short   # 期望干净或仅含本次 merge
git commit -m "merge: stable-id recreate dependency"   # 仅在产生 merge commit 且未自动提交时
```

---

### Task 1: `Backend.RefreshImage`（引擎原语）

**Files:**
- Modify: `internal/backend/backend.go`（接口，`Running` 方法之后）
- Modify: `internal/backend/docker/docker.go`（实现 + 纯函数）
- Modify: `internal/backend/qemu/qemu.go`、`internal/backend/k8s/k8s.go`、`internal/backend/multi/multi.go`
- Modify: `internal/sandbox/manager_test.go`（stubBackend 增加实现）
- Create: `internal/backend/docker/refresh_test.go`

- [ ] **Step 1: 写失败测试（纯函数比对逻辑）**

创建 `internal/backend/docker/refresh_test.go`：

```go
package docker

import "testing"

func TestRefreshChanged(t *testing.T) {
	cases := []struct {
		name     string
		local    string
		pulled   string
		hadLocal bool
		want     bool
	}{
		{"unchanged digest", "sha256:a", "sha256:a", true, false},
		{"changed digest", "sha256:a", "sha256:b", true, true},
		{"no local image", "", "sha256:a", false, true},
		{"unreadable pulled digest", "sha256:a", "", true, true},
	}
	for _, c := range cases {
		if got := refreshChanged(c.local, c.pulled, c.hadLocal); got != c.want {
			t.Fatalf("%s: refreshChanged(%q,%q,%v)=%v want %v", c.name, c.local, c.pulled, c.hadLocal, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/backend/docker/ -run TestRefreshChanged -count=1`
Expected: FAIL（`undefined: refreshChanged`）

- [ ] **Step 3: 实现纯函数与 docker 实现**

在 `internal/backend/docker/docker.go` 的 `Running` 方法之后加入：

```go
// refreshChanged reports whether a pulled image differs from the local copy.
func refreshChanged(local, pulled string, hadLocal bool) bool {
	if !hadLocal || local == "" || pulled == "" {
		return true
	}
	return local != pulled
}

// RefreshImage pulls ref and reports whether the local image digest changed.
func (b *Backend) RefreshImage(ctx context.Context, ref string) (bool, string, error) {
	if strings.TrimSpace(ref) == "" {
		return false, "", fmt.Errorf("image ref is empty")
	}
	local, hadLocal := b.imageDigest(ctx, ref)
	rc, err := b.cli.ImagePull(ctx, ref, types.ImagePullOptions{})
	if err != nil {
		return false, "", fmt.Errorf("image pull %s: %w", ref, err)
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc)
	pulled, _ := b.imageDigest(ctx, ref)
	return refreshChanged(local, pulled, hadLocal), pulled, nil
}

// imageDigest returns the repo digest of a local image, falling back to its ID.
func (b *Backend) imageDigest(ctx context.Context, ref string) (string, bool) {
	info, _, err := b.cli.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		return "", false
	}
	if len(info.RepoDigests) > 0 {
		return info.RepoDigests[0], true
	}
	return info.ID, true
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/backend/docker/ -run TestRefreshChanged -v -count=1`
Expected: PASS

- [ ] **Step 5: 接口与其余实现**

`internal/backend/backend.go`，接口中 `Running` 之后加：

```go
	// RefreshImage pulls ref and reports whether the local image changed.
	RefreshImage(ctx context.Context, ref string) (changed bool, digest string, err error)
```

`internal/backend/qemu/qemu.go`，`Running` 之后加：

```go
func (b *Backend) RefreshImage(ctx context.Context, ref string) (bool, string, error) {
	_ = ctx
	return false, "", fmt.Errorf("image refresh is not supported for qemu images")
}
```

`internal/backend/k8s/k8s.go`，`Running` 之后加：

```go
func (b *Backend) Running(ctx context.Context, sandboxID string) (bool, error) {
	return false, fmt.Errorf("k8s.Running: not implemented (phase 4)")
}

func (b *Backend) RefreshImage(ctx context.Context, ref string) (bool, string, error) {
	return false, "", fmt.Errorf("k8s.RefreshImage: not implemented (phase 4)")
}
```

（注意：`Running` 若已存在则只加 `RefreshImage`。）

`internal/backend/multi/multi.go`，`Running` 方法之后加：

```go
func (b *Backend) RefreshImage(ctx context.Context, ref string) (bool, string, error) {
	if isQcow2(ref) {
		return false, "", fmt.Errorf("image refresh is not supported for qemu images")
	}
	eng, err := b.dockerEngine()
	if err != nil {
		return false, "", err
	}
	return eng.RefreshImage(ctx, ref)
}
```

`unavailableBackend` 之后加：

```go
func (u unavailableBackend) RefreshImage(context.Context, string) (bool, string, error) {
	return false, "", u.err
}
```

`internal/sandbox/manager_test.go` 的 `stubBackend`：结构体字段补充

```go
	refreshRef     string
	refreshChanged bool
	refreshDigest  string
	refreshErr     error
```

并在 `Running` 方法之后加：

```go
func (b *stubBackend) RefreshImage(_ context.Context, ref string) (bool, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshRef = ref
	if b.refreshErr != nil {
		return false, "", b.refreshErr
	}
	return b.refreshChanged, b.refreshDigest, nil
}
```

- [ ] **Step 6: 编译并跑后端测试**

Run: `go build ./... && go test ./internal/backend/... -count=1`
Expected: 全部 ok

- [ ] **Step 7: 提交**

```bash
git add internal/backend/backend.go internal/backend/docker/docker.go internal/backend/docker/refresh_test.go \
        internal/backend/qemu/qemu.go internal/backend/k8s/k8s.go internal/backend/multi/multi.go \
        internal/sandbox/manager_test.go
git commit -m "feat(backend): add RefreshImage to pull and digest-compare images"
```

---

### Task 2: `sandbox.Manager.RefreshTemplateImage`

**Files:**
- Modify: `internal/sandbox/sandbox.go`（接口）
- Modify: `internal/sandbox/manager.go`（实现）
- Create: `internal/sandbox/manager_refresh_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/sandbox/manager_refresh_test.go`：

```go
package sandbox_test

import (
	"os"
	"testing"

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
```

注意：第二个测试需要 `time` 与 `sandbox.WithTemplates`；把 `"time"` 加入 import（与 `manager_test.go` 同风格）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/sandbox/ -run TestService_RefreshTemplateImage -count=1`
Expected: FAIL（`svc.RefreshTemplateImage undefined`）

- [ ] **Step 3: 实现接口与 Service 方法**

`internal/sandbox/sandbox.go` 的 `Manager` 接口中，`Exec` 声明之后加：

```go
	// RefreshTemplateImage resolves a template ref and pulls its image,
	// reporting whether the local image digest changed.
	RefreshTemplateImage(ctx context.Context, templateRef string) (image string, changed bool, digest string, err error)
```

`internal/sandbox/manager.go`，`hostPathOwner` 之后加：

```go
// RefreshTemplateImage resolves templateRef to an image and refreshes it.
func (s *Service) RefreshTemplateImage(ctx context.Context, templateRef string) (string, bool, string, error) {
	if s.backend == nil {
		return "", false, "", fmt.Errorf("backend not configured")
	}
	image := strings.TrimSpace(templateRef)
	if s.templates != nil {
		resolved, err := s.templates.Resolve(ctx, templateRef)
		if err != nil {
			return "", false, "", fmt.Errorf("template: %w", err)
		}
		if resolved.Image != "" {
			image = resolved.Image
		}
	}
	if image == "" {
		image = s.defaultImage
	}
	changed, digest, err := s.backend.RefreshImage(ctx, image)
	if err != nil {
		return image, false, "", err
	}
	return image, changed, digest, nil
}
```

- [ ] **Step 4: 运行确认通过**

Run: `ROUNDPEN_TEST_DATABASE_URL="postgres://roundpen:roundpen@127.0.0.1:5432/roundpen_test?sslmode=disable" go test ./internal/sandbox/ -run TestService_RefreshTemplateImage -v -count=1`
Expected: 两个测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/sandbox/sandbox.go internal/sandbox/manager.go internal/sandbox/manager_refresh_test.go
git commit -m "feat(sandbox): resolve and refresh a template image via the backend"
```

---

### Task 3: `userenv.UpgradeAgent`

**Files:**
- Modify: `internal/userenv/userenv.go`（`UpgradeAgent`、`UpgradeResult`、`EnvView.Image`、`List` 填 image、`upgradeMu`）
- Modify: `internal/userenv/fake_test.go`（fake 支持刷新 + 计数删除）
- Create: `internal/userenv/upgrade_test.go`

- [ ] **Step 1: 扩展测试 fake**

`internal/userenv/fake_test.go`：`fakeSandboxes` 结构体字段补充：

```go
	refreshImage   string
	refreshChanged bool
	refreshDigest  string
	refreshErr     error
	refreshes      int
	refreshRef     string
	deletes        int
```

`Delete` 方法改为（记录删除次数）：

```go
func (f *fakeSandboxes) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	delete(f.byID, id)
	return nil
}
```

`Delete` 之后加：

```go
func (f *fakeSandboxes) RefreshTemplateImage(_ context.Context, templateRef string) (string, bool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	f.refreshRef = templateRef
	if f.refreshErr != nil {
		return "", false, "", f.refreshErr
	}
	image := f.refreshImage
	if image == "" {
		image = templateRef
	}
	return image, f.refreshChanged, f.refreshDigest, nil
}
```

- [ ] **Step 2: 写失败测试**

创建 `internal/userenv/upgrade_test.go`：

```go
package userenv

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func newUpgradeService(boxes *fakeSandboxes) (*Service, *memSlots) {
	slots := &memSlots{}
	return &Service{Store: slots, Sandboxes: boxes}, slots
}

func TestUpgradeAgent_UpToDateSkipsRebuild(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: false, refreshDigest: "sha256:same"}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning, Image: "img:1"})
	svc, _ := newUpgradeService(boxes)

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "up_to_date" || res.Digest != "sha256:same" {
		t.Fatalf("res=%+v", res)
	}
	if boxes.deletes != 0 || boxes.creates != 0 {
		t.Fatalf("expected no rebuild: deletes=%d creates=%d", boxes.deletes, boxes.creates)
	}
	if res.Environment.Slot != SlotAgent {
		t.Fatalf("environment=%+v", res.Environment)
	}
	if boxes.refreshRef != "code-agent" {
		t.Fatalf("refresh ref=%q", boxes.refreshRef)
	}
}

func TestUpgradeAgent_RebuildsWhenChanged(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: true, refreshImage: "img:2", refreshDigest: "sha256:new"}
	mapped := &sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning}
	boxes.put(mapped)
	svc, slots := newUpgradeService(boxes)
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "upgraded" {
		t.Fatalf("status=%q", res.Status)
	}
	if boxes.deletes != 1 || boxes.creates != 1 {
		t.Fatalf("expected one rebuild: deletes=%d creates=%d", boxes.deletes, boxes.creates)
	}
	if res.Image != "img:2" {
		t.Fatalf("image=%q", res.Image)
	}
}

func TestUpgradeAgent_ForceRebuildsEvenWhenUnchanged(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: false}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning})
	svc, slots := newUpgradeService(boxes)
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.UpgradeAgent(ctx, "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "restarted" || boxes.deletes != 1 || boxes.creates != 1 {
		t.Fatalf("res=%+v deletes=%d creates=%d", res, boxes.deletes, boxes.creates)
	}
	if boxes.refreshes != 1 {
		t.Fatalf("force should still refresh the image first: refreshes=%d", boxes.refreshes)
	}
}

func TestUpgradeAgent_RefreshFailureKeepsSandbox(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshErr: errors.New("registry unreachable")}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning})
	svc, slots := newUpgradeService(boxes)
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}

	_, err := svc.UpgradeAgent(ctx, "alice", true)
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if boxes.deletes != 0 || boxes.creates != 0 {
		t.Fatalf("sandbox must be untouched: deletes=%d creates=%d", boxes.deletes, boxes.creates)
	}
	if _, ok := boxes.byID["agent-1"]; !ok {
		t.Fatal("existing sandbox was removed")
	}
}

func TestUpgradeAgent_CreatesWhenAbsent(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: true}
	svc, _ := newUpgradeService(boxes)

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "created" || boxes.creates != 1 || boxes.deletes != 0 {
		t.Fatalf("res=%+v deletes=%d creates=%d", res, boxes.deletes, boxes.creates)
	}
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/userenv/ -run TestUpgradeAgent -count=1`
Expected: FAIL（`svc.UpgradeAgent undefined`）

- [ ] **Step 4: 实现**

`internal/userenv/userenv.go`：

1）`Service` 结构体加字段：

```go
	upgradeMu sync.Mutex
```

（`sync` 已在 import 中；若没有则补。）

2）`EnvView` 加字段：

```go
	Image      string `json:"image,omitempty"`
```

3）`List` 中两处取到 `sb` 的分支补上 `v.Image = sb.Image`（`Status`/`Name` 赋值旁）。

4）文件末尾加：

```go
// UpgradeResult reports what a manual agent upgrade did.
type UpgradeResult struct {
	Status      string
	Image       string
	Digest      string
	Environment EnvView
}

// UpgradeAgent refreshes the agent template image and rebuilds the user's
// Agent environment when the image changed (or force is set).
func (s *Service) UpgradeAgent(ctx context.Context, userID string, force bool) (*UpgradeResult, error) {
	if s.Sandboxes == nil {
		return nil, fmt.Errorf("sandboxes not configured")
	}
	s.upgradeMu.Lock()
	defer s.upgradeMu.Unlock()

	templateID := "code-agent"
	if t := strings.TrimSpace(s.Config.AgentTemplate); t != "" {
		templateID = t
	}
	image, changed, digest, err := s.Sandboxes.RefreshTemplateImage(ctx, templateID)
	if err != nil {
		return nil, err
	}

	if !changed && !force {
		views, err := s.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		view := EnvView{Slot: SlotAgent, Status: "absent", TemplateID: templateID, Image: image}
		for _, v := range views {
			if v.Slot == SlotAgent {
				view = v
			}
		}
		return &UpgradeResult{Status: "up_to_date", Image: image, Digest: digest, Environment: view}, nil
	}

	existed := false
	if m, err := s.Store.Get(ctx, userID, SlotAgent); err != nil {
		return nil, err
	} else if m != nil && m.SandboxID != "" {
		existed = true
		if err := s.Sandboxes.Delete(ctx, m.SandboxID); err != nil && !errors.Is(err, sandbox.ErrNotFound) {
			return nil, err
		}
	}

	sb, err := s.EnsureAgent(ctx, userID)
	if err != nil {
		return nil, err
	}
	status := "upgraded"
	switch {
	case !existed:
		status = "created"
	case force && !changed:
		status = "restarted"
	}
	return &UpgradeResult{
		Status: status,
		Image:  sb.Image,
		Digest: digest,
		Environment: EnvView{
			Slot: SlotAgent, SandboxID: sb.ID, TemplateID: templateID,
			Status: string(sb.Status), Name: sb.Name, Image: sb.Image,
		},
	}, nil
}
```

注意状态语义：`force=true` 且镜像恰好有变化时返回 `upgraded`（有真实升级）；`force=true` 且未变化时返回 `restarted`；沙盒原本不存在时统一返回 `created`。

- [ ] **Step 5: 运行确认通过**

Run: `go test ./internal/userenv/ -count=1`
Expected: 全部 ok（含既有测试）

- [ ] **Step 6: 提交**

```bash
git add internal/userenv/userenv.go internal/userenv/fake_test.go internal/userenv/upgrade_test.go
git commit -m "feat(userenv): add UpgradeAgent with digest check and force rebuild"
```

---

### Task 4: HTTP 接口 `POST /v1/me/environments/agent/upgrade`

**Files:**
- Modify: `internal/api/envapi/handler.go`
- Create: `internal/api/envapi/handler_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/api/envapi/handler_test.go`：

```go
package envapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type fakeEnvs struct {
	force   bool
	calls   int
	result  *userenv.UpgradeResult
	err     error
}

func (f *fakeEnvs) List(context.Context, string) ([]userenv.EnvView, error) { return nil, nil }
func (f *fakeEnvs) EnsureBrowser(context.Context, string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{ID: "browser-1", Status: sandbox.StatusRunning}, nil
}
func (f *fakeEnvs) EnsureAgent(context.Context, string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{ID: "agent-1", Status: sandbox.StatusRunning}, nil
}
func (f *fakeEnvs) UpgradeAgent(_ context.Context, userID string, force bool) (*userenv.UpgradeResult, error) {
	f.calls++
	f.force = force
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &userenv.UpgradeResult{Status: "up_to_date", Image: "img:1", Digest: "sha256:x"}, nil
}

func upgradeRequest(t *testing.T, envs *fakeEnvs, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&Handler{Envs: envs}).Mount(mux)
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/me/environments/agent/upgrade", rdr)
	req = req.WithContext(auth.WithUser(req.Context(), &storage.User{Username: "alice", Role: storage.RoleUser}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestUpgradeAgentEndpoint_OK(t *testing.T) {
	envs := &fakeEnvs{result: &userenv.UpgradeResult{Status: "upgraded", Image: "img:2", Digest: "sha256:y"}}
	rec := upgradeRequest(t, envs, `{"force":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Status string `json:"status"`
		Image  string `json:"image"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "upgraded" || out.Image != "img:2" {
		t.Fatalf("out=%+v", out)
	}
	if envs.force {
		t.Fatal("force should be false")
	}
}

func TestUpgradeAgentEndpoint_ForceFlag(t *testing.T) {
	envs := &fakeEnvs{}
	rec := upgradeRequest(t, envs, `{"force":true}`)
	if rec.Code != http.StatusOK || !envs.force {
		t.Fatalf("status=%d force=%v", rec.Code, envs.force)
	}
}

func TestUpgradeAgentEndpoint_EmptyBody(t *testing.T) {
	envs := &fakeEnvs{}
	if rec := upgradeRequest(t, envs, ""); rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpgradeAgentEndpoint_ServiceError(t *testing.T) {
	envs := &fakeEnvs{err: errors.New("registry unreachable")}
	rec := upgradeRequest(t, envs, `{"force":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "registry unreachable") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/api/envapi/ -count=1`
Expected: FAIL（`UpgradeAgent` 不在 `Environments` 接口 / 路由 404）

- [ ] **Step 3: 实现**

`internal/api/envapi/handler.go`：

1）`Environments` 接口加：

```go
	UpgradeAgent(ctx context.Context, userID string, force bool) (*userenv.UpgradeResult, error)
```

2）`Mount` 加路由：

```go
	mux.HandleFunc("POST /v1/me/environments/agent/upgrade", h.upgradeAgent)
```

3）`ensureAgent` 之后加：

```go
func (h *Handler) upgradeAgent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	var body struct {
		Force bool `json:"force"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	res, err := h.Envs.UpgradeAgent(r.Context(), user.Username, body.Force)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      res.Status,
		"image":       res.Image,
		"digest":      res.Digest,
		"environment": res.Environment,
	})
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/api/envapi/ -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/api/envapi/handler.go internal/api/envapi/handler_test.go
git commit -m "feat(envapi): expose POST /v1/me/environments/agent/upgrade"
```

---

### Task 5: Web —— 设置页「Agent 环境」区块

**Files:**
- Modify: `web/src/api.ts`（`EnvironmentView.image`、`environments.upgradeAgent`）
- Modify: `web/src/lib/appNav.ts`（新增 `agent` section）
- Modify: `web/src/i18n/en.ts`、`web/src/i18n/zh_CN.ts`
- Create: `web/src/components/AgentEnvironmentPanel.tsx`
- Modify: `web/src/pages/SettingsPage.tsx`

- [ ] **Step 1: api.ts**

`EnvironmentView` 加字段：

```ts
  image?: string
```

`environments` 对象中 `ensureAgent` 之后加：

```ts
  upgradeAgent: (force = false) =>
    api<{ status: string; image: string; digest: string; environment: EnvironmentView }>(
      '/v1/me/environments/agent/upgrade',
      { method: 'POST', body: JSON.stringify({ force }) },
    ),
```

- [ ] **Step 2: appNav.ts**

`SettingsSectionKey` 加 `| 'agent'`，`SETTINGS_SECTIONS` 在 `{ key: 'git', ... }` 之后加：

```ts
  { key: 'agent', labelKey: 'settings.section.agent' },
```

- [ ] **Step 3: i18n**

`web/src/i18n/en.ts` 加：

```ts
  'settings.section.agent': 'Agent environment',
  'agentEnv.title': 'Agent environment',
  'agentEnv.hint':
    'Upgrade rebuilds the Agent container from the current template image. The workspace is kept; running commands are interrupted.',
  'agentEnv.status': 'Status',
  'agentEnv.image': 'Image',
  'agentEnv.upgrade': 'Check & upgrade',
  'agentEnv.force': 'Force rebuild',
  'agentEnv.confirmUpgrade': 'Pull the latest image and rebuild only if it changed?',
  'agentEnv.confirmForce': 'Rebuild the Agent container now? Running commands will be interrupted.',
  'agentEnv.upToDate': 'Already up to date.',
  'agentEnv.upgraded': 'Agent environment upgraded.',
  'agentEnv.restarted': 'Agent environment rebuilt.',
  'agentEnv.created': 'Agent environment created.',
  'agentEnv.failed': 'Upgrade failed.',
  'agentEnv.loadFailed': 'Failed to load environment.',
```

`web/src/i18n/zh_CN.ts` 加对应中文：

```ts
  'settings.section.agent': 'Agent 环境',
  'agentEnv.title': 'Agent 环境',
  'agentEnv.hint': '升级会用当前模板镜像重建 Agent 容器；工作区保留，运行中的命令会中断。',
  'agentEnv.status': '状态',
  'agentEnv.image': '镜像',
  'agentEnv.upgrade': '检查并升级',
  'agentEnv.force': '强制重建',
  'agentEnv.confirmUpgrade': '拉取最新镜像，若有变化才重建？',
  'agentEnv.confirmForce': '立即重建 Agent 容器？运行中的命令会中断。',
  'agentEnv.upToDate': '已是最新。',
  'agentEnv.upgraded': 'Agent 环境已升级。',
  'agentEnv.restarted': 'Agent 环境已重建。',
  'agentEnv.created': 'Agent 环境已创建。',
  'agentEnv.failed': '升级失败。',
  'agentEnv.loadFailed': '加载环境失败。',
```

- [ ] **Step 4: 组件**

创建 `web/src/components/AgentEnvironmentPanel.tsx`：

```tsx
import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Banner, Button, Modal, Typography } from '@douyinfe/semi-ui-19'
import { environments, type EnvironmentView } from '../api'
import { useT, type MessageKey } from '../i18n'

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const STATUS_KEYS: Record<string, MessageKey> = {
  up_to_date: 'agentEnv.upToDate',
  upgraded: 'agentEnv.upgraded',
  restarted: 'agentEnv.restarted',
  created: 'agentEnv.created',
}

export function AgentEnvironmentPanel() {
  const t = useT()
  const [view, setView] = useState<EnvironmentView | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await environments.list()
      setView((res.environments ?? []).find((v) => v.slot === 'agent') ?? null)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('agentEnv.loadFailed'))
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  const upgrade = useCallback(
    async (force: boolean) => {
      setBusy(true)
      setNotice(null)
      setError(null)
      try {
        const res = await environments.upgradeAgent(force)
        setNotice(t(STATUS_KEYS[res.status] ?? 'agentEnv.upgraded'))
        await load()
      } catch (e) {
        setError(e instanceof Error ? e.message : t('agentEnv.failed'))
      } finally {
        setBusy(false)
      }
    },
    [t, load],
  )

  const confirmUpgrade = useCallback(
    (force: boolean) => {
      Modal.confirm({
        title: t('agentEnv.title'),
        content: force ? t('agentEnv.confirmForce') : t('agentEnv.confirmUpgrade'),
        onOk: () => upgrade(force),
      })
    },
    [t, upgrade],
  )

  return (
    <div style={sectionGap}>
      <Typography.Text type="tertiary">{t('agentEnv.hint')}</Typography.Text>
      {error && (
        <Banner type="danger" description={error} closeIcon={null} />
      )}
      {notice && <Banner type="success" description={notice} closeIcon={null} />}
      <div>
        <Typography.Text strong>{t('agentEnv.status')}: </Typography.Text>
        <Typography.Text>{view?.status ?? 'absent'}</Typography.Text>
        {'  '}
        <Typography.Text strong>{t('agentEnv.image')}: </Typography.Text>
        <Typography.Text>{view?.image ?? '—'}</Typography.Text>
      </div>
      <div style={{ display: 'flex', gap: 12 }}>
        <Button theme="solid" loading={busy} onClick={() => confirmUpgrade(false)}>
          {t('agentEnv.upgrade')}
        </Button>
        <Button loading={busy} onClick={() => confirmUpgrade(true)}>
          {t('agentEnv.force')}
        </Button>
      </div>
    </div>
  )
}
```

- [ ] **Step 5: 挂载到设置页**

`web/src/pages/SettingsPage.tsx`：

1）import 区加：

```tsx
import { AgentEnvironmentPanel } from '../components/AgentEnvironmentPanel'
```

2）`{section === 'git' && <GitCredentialsPanel />}` 之后加：

```tsx
        {section === 'agent' && <AgentEnvironmentPanel />}
```

- [ ] **Step 6: 构建验证**

Run: `cd web && npm run build`
Expected: 构建成功（TypeScript 通过；i18n key 类型由 `translate.ts` 的 `keyof` 推导）

- [ ] **Step 7: 开发环境手动验证**

Run: `cd web && npm run dev`（或用户已在 19000 端口的 vite）
Expected: 设置页出现「Agent 环境」区块；显示 agent 槽位状态与镜像；点击「检查并升级」弹出确认，确认后按钮 loading，完成后出现结果横幅；「强制重建」同理。若无法在浏览器验证，如实说明。

- [ ] **Step 8: 提交**

```bash
git add web/src/api.ts web/src/lib/appNav.ts web/src/i18n/en.ts web/src/i18n/zh_CN.ts \
        web/src/components/AgentEnvironmentPanel.tsx web/src/pages/SettingsPage.tsx
git commit -m "feat(web): add Agent environment upgrade panel to settings"
```

---

### Task 6: 门控真实 docker 测试（digest 稳定性）

**Files:**
- Create: `internal/backend/docker/refresh_image_test.go`

- [ ] **Step 1: 写测试**

```go
package docker_test

import (
	"context"
	"os"
	"testing"

	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
)

// TestRefreshImageDigestStable: pulling an already-current image reports no
// change on the second call.
func TestRefreshImageDigestStable(t *testing.T) {
	if os.Getenv("ROUNDPEN_TEST_DOCKER_LOCAL") == "" {
		t.Skip("set ROUNDPEN_TEST_DOCKER_LOCAL=1 to run against the local Docker daemon")
	}
	be, err := dockerbackend.New("", "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer be.Close()

	ctx := context.Background()
	const ref = "ghcr.io/roundpenai/code-agent:latest"
	if _, _, err := be.RefreshImage(ctx, ref); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	changed, digest, err := be.RefreshImage(ctx, ref)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if changed {
		t.Fatal("second refresh reported a change")
	}
	if digest == "" {
		t.Fatal("empty digest")
	}
}
```

- [ ] **Step 2: 运行**

Run: `ROUNDPEN_TEST_DOCKER_LOCAL=1 go test ./internal/backend/docker/ -run TestRefreshImageDigestStable -v -count=1`
Expected: PASS（首次拉取后 digest 稳定；无 env 时 skip）

- [ ] **Step 3: 提交**

```bash
git add internal/backend/docker/refresh_image_test.go
git commit -m "test(backend): cover refresh digest stability against the local daemon"
```

---

### Task 7: 全量回归与收尾

**Files:** 无新增

- [ ] **Step 1: 全量测试（无 env）**

Run: `go test ./... -count=1`
Expected: exit 0

- [ ] **Step 2: 全量测试（含测试库）**

Run: `ROUNDPEN_TEST_DATABASE_URL="postgres://roundpen:roundpen@127.0.0.1:5432/roundpen_test?sslmode=disable" go test ./... -count=1`
Expected: exit 0

- [ ] **Step 3: race 与格式**

Run: `go test -race ./internal/userenv/ ./internal/sandbox/ ./internal/backend/docker/ -count=1 && gofmt -l internal/ | head`
Expected: 全部 ok；gofmt 输出为空（若列出非本次改动文件，忽略）

- [ ] **Step 4: 构建**

Run: `make build-go && cd web && npm run build`
Expected: 成功

- [ ] **Step 5: 最终提交（如有零散修正）**

```bash
git status --short
git add -A ':!web/dist'    # 如无改动则跳过
git commit -m "chore: agent upgrade follow-ups"
```

---

## Self-Review 记录

- **Spec 覆盖**：接口契约→Task 4；`RefreshImage`→Task 1；`RefreshTemplateImage`/模板解析→Task 2；`UpgradeAgent`/`EnvView.Image`/互斥/先拉后删/四态→Task 3；UI 区块→Task 5；测试计划（userenv 四路径+无害失败、envapi、docker 纯函数+门控）→ Task 1/3/4/6；前置依赖→Task 0。
- **类型一致性**：`RefreshImage(ctx, ref) (bool, string, error)`、`RefreshTemplateImage(ctx, ref) (string, bool, string, error)`、`UpgradeResult{Status, Image, Digest, Environment}`、`EnvView.Image` 在各任务中签名一致。
- **占位符**：无 TODO/TBD；所有代码步骤为完整代码。
- **已知注意点**：Task 3 的映射写入使用 `memSlots.Upsert(ctx, userID, slot, sandboxID, templateID)`（fake_test.go 中已有）；Task 5 的组件依赖 Semi 的 `Modal.confirm` 与 `Banner`（与 GitCredentialsPanel 同源用法）；导入清单以各文件实际为准（`goimports`/编译错误会提示缺失项）。
