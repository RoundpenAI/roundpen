# 议题与任务跟踪设计

日期：2026-09-20  
状态：已对齐，待实现计划  
范围：对话中产生的工作项（议题 / 任务 / 文档）的实体化、澄清与拆解规范、存储与接口。不含多人协作、优先级与看板拖拽。

## 1. 目标与问题

**问题**

- 用户在对话里说「我想做 X」，这个意图只活在聊天记录里：换个会话、过几天就找不回来，也无法回答「上次说到哪了」。
- `docs/superpowers/{specs,plans}/` 里的 spec 与 plan 是**开发者写给仓库**的文档：进 git、跟着代码走。它们不是**运行时数据**——用户和助手在对话里读不到、写不了、不能按议题检索。
- 澄清过程中被否决的方向、拍板的理由，没有落地位置。

**目标**

- 「要做的事」成为一等实体：对话里产生、落库、有全局可引用的短 ID、跨会话可找回。
- 澄清（边界 / 方向 / 决策）与拆解（步骤）按规范进行，成果是**版本化文档**，决策可回溯。
- 任务清单可被逐个实现并更新状态，进度不再只存在于助手的口头表述里。

**原则**

1. **对话是入口，DB 是权威。** 助手在对话里建与改，控制台读写同一份数据。
2. **先澄清，再写文档。** 边界不清、方向未定、决策未拍板时不写 Spec。
3. **文档版本化。** 修订是新版本而不是覆盖，「当时为什么这么定」可查。
4. **任务粒度 = 可独立验证的改动。** 一个任务一次验证。
5. **短 ID 引用。** `ISS-12` / `TSK-34` / `DOC-88`，人和助手都用它说话。
6. **不破坏安静对话。** 建议题、更状态是短确认，不在对话里逐步播报（延续助手优先设计 §7.1）。

## 2. 概念

| 概念 | 是什么 | 不是什么 |
|------|--------|----------|
| 议题 Issue | 一件要做的事：目标、边界、状态、来源 | 不是协助单——不需要人类裁决，不是权限申请 |
| 任务 Task | 议题下一个可独立验证的实现步骤 | 不是一次对话；不重复记录执行过程 |
| 文档 Doc | 议题的 Spec / Plan，版本化 markdown | 不是仓库里 `docs/superpowers/` 的设计文档 |

关系：一个议题 → 两组文档版本线（spec / plan，各自 `v1..vn`）+ N 个任务。任务可标注它派生自哪个 plan 版本。

## 3. 工作流规范（对话中）

本节规定**助手**在什么时机做什么，是实现时提示词与控制台互相咬合的契约。

### 3.1 何时建议题

**建。** 用户表达「开始一件新的事」的意图，且满足任一条件：

- 预计实现需要 ≥2 个可独立验证的步骤；
- 需要跨会话延续（今天定方向，明天接着做）；
- 需要沉淀决策（有取舍要留痕，日后可能回看）；
- 用户在多个方案之间选择了方向。

**不建。**

- 一次问答、查状态、解释代码、跑一条命令；
- 单步即可完成且无需留档的改动（改错别字、调一个常量）；
- 用户只是探边界（「能不能做 X」）——那是在问，不是在下活。先回答；用户确认要做时再建。

**不擅自创建。** 判据模糊时问一句「要不要立个议题跟这件事？」，而不是直接建。

### 3.2 澄清三要素

建议题后、写 Spec 前，把三件事问清楚：

| 要素 | 要确定的内容 | 落点 |
|------|--------------|------|
| 边界 Scope | 做什么、**明确不做什么**、验收标准 | Spec「边界与验收」 |
| 方向 Direction | 技术路线、复用现有模块还是新建、与既有设计的关系 | Spec「方向」 |
| 决策 Decisions | 需要用户拍板的选项 + 结论 + 理由（含被否决项） | Spec「决策记录」 |

澄清手法：

- 一轮 2-4 个问题，给推荐项与权衡，不问开放大问题（用 `AskUserQuestion`）。
- 能自己查证的不问用户（读代码、读现有文档、跑一次）。
- 一轮问不完就再来一轮。**问清楚优先于写得快。**
- 已有决定性约束（用户明说「就用 X」）时不重复请示。

### 3.3 Spec 文档

澄清到位后写 `issue_docs(kind='spec')`，建议结构（不强制）：

1. 目标与问题
2. 边界（含**非目标**）
3. 方向与决策记录（含被否决的选项与理由）
4. 验收标准
5. 未决问题（若有）

写 `current` 版本后议题状态 → `specced`。停留在 `draft` 的 Spec 不算澄清完成。

### 3.4 Plan 文档

Spec 定稿后写 `issue_docs(kind='plan')`，建议结构：文件地图（改哪些文件、各自职责）+ 按序步骤（每步含验证方式）。

写 `current` 版本后议题状态 → `planned`。

### 3.5 拆任务与逐个实现

- Plan 的**每个步骤**派生一个任务（`tasks` 行），`plan_doc_id` 指向该 plan 版本，`position` 保序。
- 任务标题是动作（「加 issues 表与 key 序列」）；`detail` 写清这一步的验收方式。
- 逐个实现：取第一个未完成任务 → `in_progress` → 在会话里**真实实现并验证** → `done`。
- **不允许只改状态不干活**，也不允许一次把多个任务标 done。
- 全部任务完成 → 议题 `done`（自动流转）。
- 实现中发现某步不成立：改 Plan（新版本）并同步调整任务，不要静默改道。

### 3.6 修订

- 任何文档修订 = 新版本（`version+1`），旧版本转 `superseded`，**不删除**。
- 新版本顶部写「相对 v(n-1) 改了什么、为什么」。
- 议题方向改变必须回到 Spec：先改 Spec，再改 Plan。

### 3.7 对话里的表达

- 用短 ID 说话：「已按 ISS-12 的 Spec 拆出 5 个任务，正在做 TSK-34」。
- 建议题、写文档、更状态各一条短消息，不逐步播报（安静对话原则）。
- 议题结束后一条摘要：做成了什么、文档与任务在哪（给 `ISS-n`）。

## 4. 状态机

### 4.1 议题状态

| 状态 | 含义 |
|------|------|
| `drafting` | 澄清中，Spec 未定稿 |
| `specced` | Spec 已定稿，Plan 未定 |
| `planned` | Plan 已定稿，任务已建 |
| `in_progress` | 至少一个任务在推进 |
| `done` | 任务全部完成 |
| `cancelled` | 用户取消，不再推进 |

### 4.2 自动流转

服务端在文档 / 任务写入的**同一事务**内推进，规则如下（只前进，`done` 可回 `in_progress`）：

| 当前 | 事件 | 结果 |
|------|------|------|
| `drafting` | spec 写入 `current` | `specced` |
| `drafting` / `specced` | plan 写入 `current` | `planned` |
| 非 `in_progress` / 非 `cancelled` | 任一任务进入 `in_progress` | `in_progress` |
| `in_progress` | 全部任务 ∈ {`done`,`cancelled`} 且 ≥1 个 `done` | `done` |
| `cancelled` | 任意事件 | 不变 |

- 任务重新打开（`done` → `todo`/`in_progress`/`blocked`）触发第三条，议题从 `done` 回到 `in_progress`。
- 边界：全部任务被 cancelled 而没有一个 done 时，议题**不**自动完成——停在 `in_progress` 等用户裁决。
- `PATCH /v1/issues/{key}` 可显式设置任意状态（用户是最终裁决者）；显式设置有 `cancelled` / `done` 时写 `closed_at`。

### 4.3 任务状态

`todo → in_progress → done`，另有 `blocked`（被外部条件卡住，需在 `detail` 说明）与 `cancelled`。任务进入终态写 `done_at`。

## 5. 数据模型

### 5.1 键

- 主键 `id` = uuid 字符串（与既有表一致）。
- `key` = 可读短 ID，**安装内全局唯一、单调递增、永不复用**：`ISS-42` / `TSK-107` / `DOC-88`。
- 生成：Postgres 序列 `issue_key_seq` / `task_key_seq` / `issue_doc_key_seq`，插入语句内 `'ISS-' || nextval('issue_key_seq')` 完成，无竞态、无额外往返。
- API 与工具**同时接受 `key` 与 uuid**：`WHERE id = $1 OR key = $1`（两列各有索引）。

### 5.2 表

**issues** — 工作项主体

| 列 | 说明 |
|----|------|
| `id` / `key` | uuid / `ISS-n` |
| `user_id` | 属主（FK users CASCADE） |
| `assistant_id` | 创建它的助手（FK assistants SET NULL） |
| `session_id` | 创建它的会话，软引用不带外键（会话可能已删，同 `assist_tickets`） |
| `title` / `summary` | 标题 / 一句话摘要 |
| `status` | 见 §4.1，CHECK 约束 |
| `origin` | `chat`（对话中由助手建）/ `console`（控制台手建） |
| `created_at` / `updated_at` / `closed_at` | `closed_at` 在进入 `done`/`cancelled` 时写 |

**tasks** — 议题下的实现步骤

| 列 | 说明 |
|----|------|
| `id` / `key` | uuid / `TSK-n` |
| `issue_id` | FK issues CASCADE |
| `user_id` | 冗余属主，供列表查询直接按用户过滤 |
| `plan_doc_id` | FK issue_docs SET NULL：派生自哪个 plan 版本 |
| `position` | 议题内顺序 |
| `title` / `detail` | 动作 / 验收方式 |
| `status` | 见 §4.3，CHECK 约束 |
| `session_id` / `assistant_id` | 实现它的会话与助手 |
| `created_at` / `updated_at` / `done_at` | |

**issue_docs** — 版本化文档

| 列 | 说明 |
|----|------|
| `id` / `key` | uuid / `DOC-n` |
| `issue_id` | FK issues CASCADE |
| `task_id` | 可空：文档也可挂在某个任务下（FK tasks CASCADE） |
| `kind` | `spec` / `plan` |
| `version` | 同一议题同一 kind 内自增，`UNIQUE (issue_id, kind, version)` |
| `status` | `draft`（草稿）/ `current`（当前定稿）/ `superseded`（被新版取代） |
| `title` / `content_md` | 标题 / markdown 正文 |
| `author_type` / `assistant_id` / `session_id` | 谁在哪个会话写的（`user` / `assistant`） |
| `created_at` | |

写新版本的同一事务里把同 `(issue_id, kind)` 的原 `current` 行转 `superseded`。

### 5.3 关系与级联

- 议题属于**用户**：对话里由助手创建的议题也是用户的议题，`assistant_id` 只记录来源。多个助手共享同一份议题池——用户的跟踪器按用户隔离，不按助手隔离。
- 删除议题级联删任务与文档；删除任务级联删挂在它下面的文档。**V1 不提供硬删除接口**，取消用 `cancelled` 状态表达，避免误删文档（`ON DELETE CASCADE` 只在直接改库时生效）。

## 6. API

| Method | Path | 说明 |
|--------|------|------|
| GET | `/v1/issues?status=&assistantId=&limit=` | 当前用户的议题列表，`updated_at DESC` |
| POST | `/v1/issues` | `{title, summary?, assistantId?, sessionId?}` → 201 |
| GET | `/v1/issues/{key}` | 议题 + `docs`（**索引，不含正文**）+ `tasks` |
| PATCH | `/v1/issues/{key}` | `{status?, title?, summary?}` |
| GET | `/v1/issues/{key}/docs?kind=` | 文档索引（key/kind/version/status/title/createdAt） |
| POST | `/v1/issues/{key}/docs` | `{kind, contentMd, title?, status?, taskKey?}` → 新版本 |
| GET | `/v1/issues/{key}/docs/{docKey}` | 单份文档（含 `contentMd`）；`docKey` 支持 `DOC-n` / uuid / `latest` |
| GET | `/v1/issues/{key}/tasks` | 任务清单，按 `position` |
| POST | `/v1/issues/{key}/tasks` | `{title, detail?, position?, planDocKey?, status?}` → 201 |
| PATCH | `/v1/tasks/{key}` | `{status?, title?, detail?, sessionId?}` |

约定：`{key}` 位置一律接受短 ID 或 uuid；错误体沿用 `{"error": "..."}`（与 `assistant` 包一致）；创建 201，其余 200；400 校验失败、403 越权、404 不存在。

## 7. Agent 工具面

注册在 `internal/acp/sysagent/tools`，全部经 `RoundpenHTTP` 调上面这些 API（与 `ListSessions` 等既有工具同一条路），因此**工具不直连 DB**，权限与校验只有一份实现。

| 工具 | Mutating | 参数 | 用途 |
|------|----------|------|------|
| `CreateIssue` | 是 | `title`, `summary?` | 建议题，返回 `key` |
| `ListIssues` | 否 | `status?` | 列议题（默认未完成） |
| `GetIssue` | 否 | `key` | 议题 + 文档索引 + 任务清单 |
| `UpdateIssue` | 是 | `key`, `status?`, `title?`, `summary?` | 更状态 / 标题 |
| `WriteIssueDoc` | 是 | `key`, `kind`, `contentMd`, `title?`, `status?`, `taskKey?` | 写 Spec/Plan 新版本 |
| `ReadIssueDoc` | 否 | `key`, `kind?`, `version?` | 读文档正文（缺省取 current） |
| `CreateTask` | 是 | `issueKey`, `title`, `detail?`, `position?`, `planDocKey?` | 拆任务 |
| `UpdateTask` | 是 | `taskKey`, `status?`, `title?`, `detail?` | 推进任务状态 |

**来源绑定。** 工具在**注册期**绑定当前会话（同 `BrowserBinder` 的做法），创建议题 / 写文档 / 建任务时把该 `sessionId` 带给服务端。服务端校验会话属于当前用户后写入 `session_id`，并**从会话行（`agent_sessions.assistant_id`）反查** `assistant_id` —— 模型无法伪造来源，助手也不需要知道自己是哪个助手。控制台创建的议题记 `origin='console'`、无来源会话。

**Auto mode。** 这 8 个工具写的是用户自己的跟踪器，不产生对外副作用、不触碰沙箱外文件，属于「记录」而非「执行」。因此 `automode.Defaults()` 的 `Allow` 增加一条规则，让它们免于逐次审批；`Mutating: true` 仍然照常传入，语义上它们是写操作（会落库），分类器可按上下文拦截。

**提示词。** 规范（§3）写进 System Agent 的系统提示词（`internal/acp/sysagent/history.go` 的 `system` 常量），因为它是**always-on 行为**；不复用 `Skill` 机制（技能是按名调用、每次回复限 3 次，承载不了默认行为）。

## 8. 控制台

- 一级导航加「议题」（`nav.issues`），顺序在助手之后。
- **列表页** `/issues`：key + 标题 + 状态标签 + 来源助手 + 更新时间；状态筛选；「新建议题」（标题 + 摘要）→ 详情。空态提示「你也可以直接在对话里让助手创建议题」。
- **详情页** `/issues/:key`：头部是 `ISS-12` + 标题 + 状态 + 来源（助手 / 会话链接）。
  - 左：Spec / Plan 两个页签，版本下拉（标注 `current` / `superseded` / `draft`），Markdown 正文（Semi `MarkdownRender`，零新依赖），「新版本」入口。
  - 右：任务清单，勾选即 `done`，可新增；显示 `blocked` 需说明。
- 对话里不做额外卡片：助手调用工具会产生工具消息，已由既有对话渲染承载。

## 9. 与既有概念的关系

| 既有 | 与议题的区别 |
|------|--------------|
| `assist_tickets`（协助单） | 协助单是**人在环的裁决请求**（权限、验证码），由助手发起、等人处理，处理后写回助手宪法。议题是**要做的活**，由用户意图驱动、由助手执行。二者不互相替代：一个议题的实现过程中可能需要多张协助单。 |
| `docs/superpowers/{specs,plans}` | 仓库内设计文档：给开发者读、进 git、随代码演进。`issue_docs` 是运行时数据：给用户与助手读、进 DB、随对话演进。一个功能可以两者都有，不互相替代。 |
| `memory_long` | 长期记忆存事实 / 偏好 / 情节，是「知道什么」；议题是「要做什么」。议题完成后结论可沉淀为记忆，但不是同一层。 |
| `browser_tasks` | 浏览器一次性作业，不是工作项。 |

## 10. 权限与隔离

- 所有查询在 **SQL 层**强制 `user_id` 过滤，handler 再按 `auth.GetUser` 校验属主（沿用 `agentapi` 的 `sess.UserID != user.Username && user.Role != "admin"` 模式）。
- 助手工具走 `X-API-Key`，actor 即用户，因此助手只能读写当前用户名下的议题。
- 文档正文是用户与助手自由撰写的 markdown：控制台渲染时必须走 `MarkdownRender`（不注入原始 HTML），避免 XSS。

## 11. 非目标

- 多人协作、指派、评论、@ 提醒。
- 优先级、里程碑、标签、看板拖拽、日程。
- 议题事件流 / 审计时间线（谁在何时改了什么）——列为后续计划。
- 议题之间的依赖图、父子议题。
- 硬删除接口、回收站。
- 附件、图片、富文本编辑。
- 与 GitHub / GitLab issue 双向同步。
- 自动派活：议题自动分配给某个助手。

## 12. 成功标准

- 用户说「我想做 X」，助手先说清边界 / 方向 / 决策，再落 Spec；用户能看到文档从 v1 到 v2 的演进与理由。
- 一个议题从对话 → Spec → Plan → 任务清单 → 逐个完成，全程用 `ISS-n` / `TSK-n` 指代，跨会话不丢状态。
- 换会话或重开控制台后，议题、文档、任务状态与 DB 一致。
- 助手不会为一次问答或单步改动建议题，也不会在对话里逐步播报状态。
