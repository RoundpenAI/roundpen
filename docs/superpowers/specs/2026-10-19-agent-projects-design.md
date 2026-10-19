# Agent 项目（Projects）设计与缺口分析

日期：2026-10-19  
状态：Phase A 已落地；P1 最小派活进行中  
参考：[Cursor Projects](https://cursor.com/docs/agent/projects)

范围：一级菜单「项目」、新建 Project、关联仓库、Coordinator 协调多个 Agent 完成较大事项。  
不含：计费套餐门控、Privacy Mode 云侧禁令（Roundpen 默认自托管）。

---

## 1. Cursor Projects 能力模型（对照基准）

| 能力 | Cursor 做法 | 一句话 |
|------|-------------|--------|
| 入口 | Agents Window 左侧 Projects → New Project | 一级导航实体 |
| 创建 | 图标 + 名称；选 Workspace（仓库）+ Coordinator 模型 | 绑定云端可用仓库 |
| Coordinator | 只规划/派活，不写代码；对用户保持可对话 | 长期会话入口 |
| Worker Agents | Coordinator 按需并行创建；用户可点进任一 worker | 多 Agent 共用一事 |
| 运行环境 | 默认 Cloud Agent（关电脑不停）；需要时拉起 local agent | 并行度不受本机限制 |
| Shared Context | 项目级文件集，跨 cloud/local/worker 同步增长 | 不用每次重新 onboarding |
| Subscriptions | 监听 Slack / 日程 / PR / CI，无提示也可行动 | Listening pill |
| Agent 互通 | Coordinator 可读/发消息给 @ 提到的 agent | 跨会话协调 |

---

## 2. Roundpen 现状（可复用 vs 空缺）

### 2.1 一级导航

现有：`助手` / `事项` / `文件` / `设置` / `系统管理`（`web/src/lib/appNav.ts`）。  
**没有** `项目` 菜单、路由、API、表。

### 2.2 「project」一词今天的含义

| 现有 | 含义 | 与 Cursor Project 的关系 |
|------|------|-------------------------|
| `/workspace/projects/<name>/` | 账号工作区里「一个仓库/工程一个目录」的**文件系统约定** | 可作 Project 的默认落地路径，**不是**产品实体 |
| 助手（Assistant） | 用户侧身份档案；可多开；system 助手简介写「派活后续上线」 | 可当 Coordinator / Worker 的人格载体 |
| Agent 会话 / 环境槽 | 执行层；**每用户一个 Agent 槽**，非每助手一容器 | 并行 worker 会撞槽位模型 |
| 事项 Issue + Task | 一次较大工作的跟踪（spec/plan/tasks） | 适合挂在 Project 下，不是 Project 本身 |
| 常驻任务 Routine | 单助手 + cron 反复执行 | 接近 Subscriptions 的「日程」子集 |
| Git 凭据 | 设置里配 GitHub/GitLab 等 | 有凭据，**无**「仓库 ↔ 实体」绑定表 |
| 账号级 Shared workspace | 所有助手共享 `/workspace` | 粗粒度共享；缺「项目级 Context」隔离与增长协议 |

### 2.3 已规划但未落地（设计文档里写过）

- 按简介**自动派活**（assistant-first / routines 明确排除）
- 议题**自动分配**助手（issue 设计排除）
- 任务依赖图
- 多用户协作同一助手

这些与 Projects 的 Coordinator 派活直接相关，需要一并纳入本能力，而不是另开三条线。

---

## 3. Roundpen 建议的产品模型

```
用户
 └─ Project（产品实体，一级菜单）
     ├─ repo：关联仓库（URL + 本地 path，默认 projects/<slug>/）
     ├─ coordinator：一个 assistant（建议 system 或专用 kind）会话
     ├─ workers[]：Coordinator 拉起的执行会话（可并行）
     ├─ issues[]：本项目下的 ISS / TSK（可选过滤）
     ├─ context/：项目级 Shared Context（目录或 DB+文件）
     └─ subscriptions[]：Slack / cron / PR / CI（分期）
```

与现有实体边界：

| 实体 | 继续做什么 | Project 出现后 |
|------|------------|----------------|
| 助手 | 人格、能力、网络档、IM | Coordinator / Worker 仍是助手或会话挂在助手上 |
| 事项 | 单次工作的 spec/plan/task | 可 `project_id` 归属；列表可按项目过滤 |
| 常驻任务 | 单助手周期任务 | 可升级为 Project Subscription，或暂时挂在 Coordinator 下 |
| 文件 `/workspace/projects/` | 仓库落盘 | Project.repoPath 指向该目录；菜单「项目」管元数据，「文件」仍看盘 |

---

## 4. 缺口清单（需要补充的事情）

按依赖分层。MVP 建议只做 **P0**；P1/P2 对齐 Cursor 完整体验。

### P0 — 能建项目、绑仓库、有 Coordinator 对话入口

| # | 缺口 | 说明 | 建议落点 |
|---|------|------|----------|
| P0-1 | **Project 领域模型 + 表** | `id/key/user_id/name/icon/repo_url/repo_path/coordinator_assistant_id/coordinator_session_id/status/...` | `migrations/00xx_projects.sql` + `internal/project` |
| P0-2 | **CRUD API** | `GET/POST /v1/projects`，`GET/PATCH/DELETE /v1/projects/{id}` | `internal/api` 或 `internal/project` HTTP |
| P0-3 | **一级菜单 + 路由** | `nav.projects` → `/projects`，详情 `/projects/:id` | `appNav.ts`、`AppShell`、`App.tsx`、i18n、e2e |
| P0-4 | **新建 Project UI** | 名称、图标、选仓库（或「稍后 clone / 已有 path」）、Coordinator 模型/助手 | `web/src/pages/ProjectsPage.tsx` + Create 弹窗 |
| P0-5 | **仓库关联** | 创建时：选已有 `projects/<name>` **或** 填 remote + clone 到规范路径；写入 `repo_url` + `repo_path` | 复用 gitcred；可选调用 workspace/files 或 Agent clone |
| P0-6 | **打开即进 Coordinator 聊天** | 创建后确保 coordinator session；详情页主区 = 会话（对齐 Cursor「打开就是和 coordinator 聊」） | 复用 `ChatSessionPage` / session API，会话 `kind` 扩展 |
| P0-7 | **命名消歧** | UI「项目」= 产品实体；文档明确与 `projects/` 目录的关系，避免混用 | `workspace-layout.md`、AGENTS.md 补一句 |

### P1 — 多 Agent 协调（Cursor 核心差异）

| # | 缺口 | 说明 | 建议落点 |
|---|------|------|----------|
| P1-1 | **Coordinator 角色** | 规划/拆解/验收，默认不直接改代码；system prompt + 工具集与 worker 区分 | assistant `kind` 或 session role：`coordinator` / `worker` |
| P1-2 | **派活（Delegate）** | Coordinator 创建 worker 会话：brief、挂 repo_path、可选挂 issue/task；用户可点进 worker | 新工具或控制面 API；打通已规划的「派活」 |
| P1-3 | **并行执行与槽位** | 今日 **每用户一个 Agent 环境槽**；并行 worker 需要：排队、多槽、或同槽多会话串行策略 | `user_environments` / automode 策略决策（产品必须先定） |
| P1-4 | **项目 ↔ 事项** | `issues.project_id`；Coordinator 用现有 issue/task 工具但默认 scoped 到本项目 | migration + issue store 过滤 |
| P1-5 | **Worker 列表 UI** | 项目详情侧栏：Coordinator + 进行中/已完成 workers | Project 详情布局 |
| P1-6 | **结果汇总** | Worker 完成后摘要回 Coordinator；用户在 Coordinator 处验收 | session 事件 / assist ticket 模式复用 |

### P2 — Shared Context / Subscriptions / 互通（对齐 Cursor 完整面）

| # | 缺口 | 说明 | 建议落点 |
|---|------|------|----------|
| P2-1 | **项目级 Shared Context** | 独立于账号根 `AGENTS.md`：`projects/<slug>/.roundpen/context/` 或 `project_context` 表；worker 启动时注入 | 布局约定 + session preamble |
| P2-2 | **Context 增长协议** | Worker/Coordinator 可写入「测试怎么跑、偏好」；后续会话自动读到 | 工具 + 文档合同 |
| P2-3 | **Subscriptions** | Slack / cron / PR / CI 唤醒 Coordinator | 扩展 routines：`scope=project`；connectors + git webhook |
| P2-4 | **Listening UI** | 输入框上方 Listening pill，列出/删除订阅 | 前端 + API |
| P2-5 | **Agent 间消息** | @agent 读写 transcript、互发消息 | ACP/控制面新通道（工作量大） |
| P2-6 | **Local vs Cloud** | Cursor 云默认 + 本机测试；Roundpen 已是自托管「常驻机器」——需产品文案：默认即「关控制台不停」（容器在 NAS/服务器上） | 文档；若要做「用户笔记本侧」再另开 local agent |

### 横切 / 非功能

| # | 缺口 | 说明 |
|---|------|------|
| X-1 | **权限** | Project 归属 `user_id`；与现有助手/事项一致，暂不做团队共享 |
| X-2 | **删除策略** | 删 Project：是否删 `projects/<slug>/` 磁盘、是否级联 issues/sessions | 建议：元数据删、磁盘默认保留 |
| X-3 | **与「文件」页关系** | 项目详情可深链到 `/workspace?path=projects/foo`；文件页不替代项目管理 |
| X-4 | **移动端** | `mobile/` 是否同期加入口 | 可后置 |
| X-5 | **测试** | API 单测、导航 e2e、创建流程 e2e | `appNav.test.ts`、`nav.spec.ts`、新建 `projects.spec.ts` |

---

## 5. 与现有路线的衔接（避免重复造轮）

1. **派活**：assistant-first 里「按简介自动派活」可降级为 Project 内 **人工确认后的 Coordinator 指派**，再演进自动匹配。  
2. **事项**：Issue 继续做「一次交付」；Project 做「跨月、多 PR、多会话」容器。大功能 = 1 Project + 多个 ISS。  
3. **常驻任务**：cron 类 Subscription 先复用 Routine 模型，加 `project_id`；Slack/PR/CI 另接事件源。  
4. **目录 `projects/`**：继续当 clone 根；产品 Project.slug ↔ 目录名建议 1:1。

---

## 6. 建议落地顺序

```
Phase A (P0)  菜单 + 表 + CRUD + 绑仓库 + Coordinator 会话
      ↓
Phase B (P1)  Delegate worker + 项目 scoped issues + 槽位策略
      ↓
Phase C (P2)  Shared Context → Subscriptions → Agent 互通
```

**Phase A 验收**：侧栏有「项目」→ 新建 → 关联 `projects/foo` 或 remote → 打开与 Coordinator 对话；仓库路径对 Agent 可见。  
**Phase B 验收**：Coordinator 能拆出至少 2 个 worker 会话（可先串行），用户能从项目页点进每个 worker，事项可挂在项目下。  
**Phase C 验收**：项目 Context 跨会话保留；至少一种订阅（建议先 cron）能无提示唤醒 Coordinator。

---

## 7. 已拍板决策

1. **Coordinator** → 每项目一个专用 `kind=coordinator` 助手（不出现在「助手」列表；打开项目即进其会话）。  
2. **并行 worker** → 先串行队列（P1）；UI 可展示多个 worker，同时只有一个在跑。  
3. **仓库关联** → `repo_path`（`projects/<slug>`）必有；`repo_url` 可选。创建不强制 clone。GitHub 未配好时，直接在本地目录 / 已有 git 仓库上工作。  
4. **Shared Context** → 默认旁路 `projects/<slug>/.roundpen/context/`，不进 git。

---

## 8. 结论与 Phase A 范围

Roundpen 原先没有一等公民 Project。P0（Phase A）落地：

- 表 / API / 一级菜单「项目」
- 新建（名称、slug、可选 remote）
- 绑定本地 `projects/<slug>`，尽力播种目录与 context
- 创建专用 coordinator 助手；详情页打开其聊天

P1/P2 见上文缺口表（派活、槽位、Subscriptions 等）。
