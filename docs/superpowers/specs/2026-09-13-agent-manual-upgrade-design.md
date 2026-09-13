# 设计：Agent 环境手动升级（用户自助）

> 日期：2026-09-13
> 状态：已评审待实施
> 范围：`userenv` / `backend`（镜像刷新原语）/ `envapi` / 设置页 UI

## 背景与目标

镜像发布到 ghcr 后（如 code-agent 更新），已部署实例的老用户其 Agent 容器仍运行旧镜像。当前系统完全没有更新路径：`docker.ensureImage` 只在本地缺失时拉取（`internal/backend/docker/docker.go`），镜像一旦存在就永不更新；界面也没有任何提示或操作。

目标：给用户一个**手动升级入口**，把它自己的 Agent 环境更新到当前配置解析出的镜像。非目标：定时检查/自动更新、管理员批量升级、浏览器槽位升级（见"非目标"）。

## 需求结论（澄清结果）

1. **入口**：用户自助——API + 设置页新增「Agent 环境」区块。
2. **语义**：先检查再重建。刷新镜像并比对 digest；未变化 → 返回"已是最新"、**不重启**；有变化 → 重建。另提供 `force` 强制重建（可兼作"重启环境"）。
3. **按钮位置**：设置页（与 llmgw / git 等既有区块同级）。

## 方案选择

采用**组合式（方案 A）**：升级 = 刷新镜像 + 复用现有"删除 → `EnsureAgent`"重建路径。

- 复用面最大：镜像解析、容器 Env 组装、稳定 id 重建（storage 复活软删除行）、git 凭据注入、状态对账与自愈，全部是既有且已被测试覆盖的代码。
- 备选方案 B（manager 级原地滚动重建 `Rebuild(id, image)`）被否：容器的 Env 未持久化在沙盒行中（gateway 地址、虚拟密钥、`ROUNDPEN_SLOT` 等由 `userenv.createSlot` 组装），原地重建要么变更存储语义（Env 落 metadata），要么引入 env 回调，改动面与回归风险大，收益仅是保留 `created_at`。
- 方案 C（只拉取不重建）由用户否掉。

## 设计

### 1. 接口契约

```
POST /v1/me/environments/agent/upgrade
Body（可选）: {"force": false}
```

响应（200）：

```json
{
  "status": "up_to_date" | "upgraded" | "restarted" | "created",
  "image": "ghcr.io/roundpenai/code-agent:0.1.0",
  "digest": "sha256:...",
  "environment": { "slot": "agent", "status": "running", "image": "..." }
}
```

行为矩阵：

| 条件 | 动作 | status |
|------|------|--------|
| force=false，digest 未变化 | 不重建 | `up_to_date` |
| force=false，digest 变化 | 删旧容器 → 重建 | `upgraded` |
| force=true | 不比对，直接重建 | `restarted` |
| agent 沙盒不存在 | 直接创建（等价 ensure） | `created` |

约定：

- **先拉取成功、再删旧容器**：拉取/探测失败返回 5xx，旧环境保持不动。
- 同步返回（与既有 ensure 接口一致）；拉取耗时由前端按钮 loading 承担。
- 进程内全局升级互斥（升级是低频操作，全局锁足够，避免并发删除/重建互相踩踏）。
- 与并发 `EnsureAgent` 的竞争：退化路径自愈——稳定 id 重建最终一致（storage 复活行 + create 冲突回退到 resolve/adopt 已有处理）。

### 2. 服务层

**新增 `backend.Backend` 原语**

```go
// RefreshImage 拉取 ref 的最新版本并报告 digest 是否变化。
RefreshImage(ctx context.Context, ref string) (changed bool, digest string, err error)
```

- docker 实现：`ImageInspectWithRaw` 取本地 `RepoDigests`（镜像不存在视为 changed）→ `ImagePull` → 再 inspect 取新 digest 比对；任一步失败返回错误。
- qemu：返回 `unsupported` 错误（agent 槽位固定 Docker 引擎，正常不会走到）。
- k8s：`not implemented`（同现有风格）。multi：按引擎分发。

**新增 `userenv.UpgradeAgent`**

```go
type UpgradeResult struct {
    Status      string // up_to_date | upgraded | restarted | created
    Image       string
    Digest      string
    Environment EnvView
}

func (s *Service) UpgradeAgent(ctx context.Context, userID string, force bool) (*UpgradeResult, error)
```

流程：

1. 解析镜像：与 `EnsureAgent` 同源（`code-agent` 模板或 `Config.AgentTemplate` 的 `Resolve` 结果）。
2. `RefreshImage`（**force 也先拉取**，避免"强制重建了一个旧镜像"）。
3. `changed || force` 时重建：先尝试删除在册的 agent 沙盒（软删 + 移除容器；持久 workspace 不删，`sandbox.Service.Delete` 已有该判断），然后 `EnsureAgent`（不存在沙盒时跳过删除，直接创建；重建时重新注入 git 凭据）。
4. 未变化且非 force → 不触碰容器，返回 `up_to_date`。

### 3. UI（设置页「Agent 环境」区块）

- 显示：槽位状态（running / stopped / absent）、当前镜像 ref（`EnvView` 增加 `image` 字段；absent 时展示将要使用的模板镜像）、最近一次升级结果。
- 操作：
  - 主按钮「检查并升级」→ `{"force": false}`；
  - 次按钮「强制重建」→ `{"force": true}`；
  - 点击后确认弹窗（说明会重启环境、进行中的命令会中断）。
- 反馈：成功/已最新/失败用 Message 提示并刷新；按钮在请求期间禁用。
- 文案：`web/src/i18n/en.ts` 与 `zh_CN.ts` 同步新增 key。

### 4. 数据流（upgraded 路径）

```
用户点击 → POST upgrade → userenv.UpgradeAgent
  → 模板 Resolve(image) → backend.RefreshImage(pull+digest)
  → changed? ── no → 200 up_to_date（容器不动）
             └ yes → sandbox.Delete（软删+rm 容器，workspace 保留）
                     → EnsureAgent（新容器：新镜像/属主身份/env 重新组装/git 注入）
                     → 200 upgraded + environment 视图
```

## 非目标（本期不做）

- 定期检查更新 / 自动升级（后续可在此接口上加"有新版本"提示与定时比对）。
- 管理员批量升级全部用户（可在 `UpgradeAgent` 上包一层循环，留待后续）。
- 浏览器（QEMU）槽位与任意沙盒的通用"升级"。
- 镜像拉取策略设置项（`if-not-present` / `always`）。

## 测试计划

- `userenv`（fake sandboxes + fake 刷新器）：
  - digest 未变化 → 不调用 delete/ensure，返回 `up_to_date`；
  - digest 变化 → 调用 delete + ensure，返回 `upgraded`；
  - force → 直接重建，返回 `restarted`；
  - 刷新失败 → 返回错误且**未删除旧沙盒**；
  - 升级后 workspace 目录仍存在（持久工作区不被清理）。
- `envapi`：handler 测试（mock service）：200 响应体字段；服务错误 → 5xx 映射。
- `backend/docker`：digest 比对逻辑抽纯函数单测；docker 实际行为用门控真实测试（`ROUNDPEN_TEST_DOCKER_LOCAL=1`，复用 `exec_cancel_test.go` 的模式）验证"拉取已有镜像 digest 不变"。
- e2e（可选）：Playwright 冒烟——按钮存在、点击后环境状态刷新。

## 依赖与前置

- 依赖已修的**稳定 id 重建链**：`fix/agent-container-user` 中的 storage 复活（软删除行同 id 重建）+ 容器以属主身份运行。实施前需该分支合并进 master（或本功能分支 stack 在其上）。
- `EnvView` 增加 `image` 字段是向后兼容的小改动（前端可选消费）。

## 风险与边界

- 重建瞬间运行中的命令/助手会话被中断：工具调用会以失败终态收尾（cancel/取消路径已修，自然出错路径本就发送终态）；stdio provider 运行时由既有 stale-runtime 清理回收，下条消息自动重连。
- 容器内 `/workspace` 之外的状态（安装的包、临时进程）随重建丢失——与现有"删除重建"语义一致，UI 确认弹窗会写明。
- 私有镜像仓库：与新建沙盒同源要求（引擎侧凭据 / `ROUNDPEN_AGENT_IMAGE` 指向私有镜像），本功能不新增要求。
