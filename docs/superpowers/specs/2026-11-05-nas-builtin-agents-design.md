# NAS 内置助手设计

日期：2026-11-05  
状态：已对齐方向，待实现计划  
范围：面向家用 / NAS（含 fnOS）的内置助手场景包——相册回忆、文件整理、影视与音乐「搜源补全 + 驱动下载」。  
不含：通用播放器产品、版权内容分发、内置盗版站点目录、把 host 整盘无确认地交给 Agent 乱写。

相关现状：

- 助手 / 目录授权：[assistant-first UI](2026-09-12-assistant-first-ui-design.md)
- Host 只读访问：`internal/hostaccess`（grant 有 `readwrite` 字段，写尚未落地）
- 往年今日雏形：`internal/proactive/onthisday.go`（文件名 / mtime，非 EXIF）
- 常驻任务：[standing routines](2026-10-01-standing-routines-design.md)
- Browser / WebSearch：已有工具面；下载中心 / 飞牛媒资 API 尚未做成 Agent 工具

---

## 1. 目标与原则

**问题**

Roundpen 已能装到 NAS，也能给助手授目录、用 Browser / 搜索上网，但缺少「家里硬盘上的事」专用人格与平台能力：回忆照片只会进 proactive 摘要；文件整理不会写 host；影视音乐缺「找源 → 下下来」闭环。

**目标**

1. 用户一键启用 **NAS 场景包**，得到 3～4 个预配置助手（可单独关）。
2. **相册助手**：往年今日 / N 年前今日，可浏览、可整理进目标目录或回忆集合。
3. **文件助手**：批量重命名、查重、目录重规划；默认 dry-run，确认后执行。
4. **影视助手 / 音乐助手**：核心价值是**搜源与补全缺失**，再驱动本机下载工具落盘；播放只做顺带（打开路径 / 深链到系统播放页）。
5. 共用平台基建：host 写、批量变更闸、下载器连接器；不各做一套运行时。

**原则**

1. **助手是人格，平台是能力。** 场景包主要是 prompt + 能力开关 + 默认 grants + 技能；写盘 / 下载走控制面工具。
2. **可读可建议 ≠ 可乱改。** Host 写与下载提交一律：计划 → 预览 → 确认（automode / AskUser 可对受信规则放宽，默认偏紧）。
3. **搜源用 Agent + Browser；落地用下载器。** Agent 不在沙箱里直接灌大文件到 `/workspace`，而是向用户已配置的下载后端投递任务（保存路径落在 directory grants 内）。
4. **不内置片源目录。** Roundpen 不维护「去哪下」的站点白名单产品；搜哪些站由用户网络档、Browser、自备索引器 / MCP 决定。用户对来源合法性自负。
5. **按登录态默认，不按安装包猜。** `authProvider=fnos`（飞牛网关登录，用户名 `fnos-<uid>`）的账号**默认启用**场景包；本机密码 / OAuth 等非 fnOS 登录默认不创建，可在设置里手动开。

---

## 2. 产品决策摘要

| 主题 | 决策 |
|------|------|
| 交付形态 | **NAS 场景包**；fnOS 登录默认开，非 fnOS 默认关、可手动开 |
| 助手数量 | 相册、文件、影视、音乐 四个模板；影视与音乐可合并为一个「媒资」助手（见 §8 开放项，默认**分开**以便 bio / grants 不同） |
| 运行时 | 仍用进程内 System Agent；需要重活时再 `DelegateToExpert` / Bash（文件哈希等） |
| Host 文件 | 落地 `readwrite`：Rename / Move / Copy / Mkdir / Delete（删默认强确认） |
| 相册 | 升级 OnThisDay：索引（EXIF 优先，回退文件名/mtime）+ 对话整理 + 可选 routine |
| 文件整理 | 规则重命名、内容哈希查重、目录规划；一律变更计划 |
| 影视 / 音乐 | **搜源 + 补全缺口 + 投递下载**为主；播放 = 打开文件 / 系统深链，不做自研播放器 |
| 下载 | 连接器抽象：先 qBittorrent / aria2 RPC，再 fnOS 下载中心（若官方 API 可用） |
| 播放 | P2 顺带；不阻塞搜源下载闭环 |

---

## 3. 场景包与助手模板

### 3.1 何时默认开

| 账号 | 默认 | 依据 |
|------|------|------|
| 飞牛网关登录（`AuthProvider == "fnos"`） | **开** | 与是否 `.fpk` 安装解耦：只要是 NAS SSO 进来的用户就视为家庭盘场景 |
| `local` / OAuth 等 | **关** | 笔记本、compose 开发、纯服务器部署不刷四个助手 |

触发：该用户**首次**成功建立会话（或首次进入助手列表）时 `EnsureNASScenePack`——按模板 upsert 四个助手；已存在则跳过，不覆盖用户改过的名称 / bio / grants。用户删掉某个助手后，不自动再创建，除非在设置里点「恢复缺失的 NAS 助手」。

整包关闭：设置里关掉「家庭 NAS 助手」→ 停用（`status=disabled`）四个模板助手，**不硬删**对话历史；再开则重新启用并补齐缺失模板。

### 3.2 在哪里打开 / 管理（信息架构）

**不**做一级导航，不进「系统管理」（这是每用户场景，不是整站开关）。

| 位置 | 作用 | 谁看得见 |
|------|------|----------|
| **助手侧边栏**（`/a`） | 启用后四个助手直接出现在列表里——日常「打开」就是点助手开聊 | fnOS 默认已有；其他人启用后才有 |
| **个人设置 → 工作环境** 新段落 `nas`（文案：「家庭 NAS 助手」） | 整包开/关、查看四个模板是否已创建、**恢复缺失**、链到下载器配置（M3） | **所有安装**都显示该段落；非 fnOS 默认关，说明「飞牛登录会自动开启」 |
| **新建助手**（`/a/new`） | 能力预设里**不**再塞四个 NAS 模板（避免和场景包双入口）；需要单个助手用场景包恢复或普通新建 | — |

首登体验（仅 fnOS）：侧边已有相册/文件/影视/音乐；若尚无目录授权，点进助手详情「可见范围」补 grant——不单独做堵门向导。

示意图：

```text
顶栏：助手 | 项目 | 文件 | 设置 | …
                    └─ 设置 / 工作环境 / 家庭 NAS 助手
                         ├─ [开] 飞牛登录默认开启
                         ├─ 相册 / 文件 / 影视 / 音乐 状态
                         └─ 恢复缺失的助手

助手侧边（启用后）：
  系统助手
  相册助手     ← 点这里开聊 = 「打开」场景
  文件助手
  影视助手
  音乐助手
  + 新建助手
```

### 3.3 启用时创建的模板

启用时对当前用户 upsert 助手（已存在则跳过或刷新默认技能，不覆盖用户改过的名称与 grants）：

| ID 键（逻辑） | 显示名（可改） | 能力默认 | 建议默认 grant |
|---------------|----------------|----------|----------------|
| `nas.photos` | 相册助手 | Browser 关；Shell 关或极窄 | 相册目录 `read`，回忆输出目录 `readwrite` |
| `nas.files` | 文件助手 | Shell 可选（大盘哈希）；Browser 关 | 待整理根目录 `readwrite` |
| `nas.movies` | 影视助手 | Browser + WebSearch 开；Shell 默认关 | 片库 / 下载中目录 `readwrite` |
| `nas.music` | 音乐助手 | 同影视 | 音乐库 / 下载中 `readwrite` |

`kind`：普通 `user` 助手即可（可删、可停用）；不要做成不可删 system，避免和非 NAS 用户心智冲突。场景包只是**创建模板 + 种子技能**。

### 3.4 共同行为约定（写入各助手 identity / skill）

- 先 `ListDirectoryGrants`，没有授权就请人授权，不猜盘符。
- 任何批量写、删除、下载提交前给出**变更计划**（路径列表 / 磁力或 URL / 保存目录）。
- 对话保持安静：执行中写活动日志；结束回一条摘要。
- 中文用户场景下用「事项」跟踪多日补全任务（例如整季追更）。

---

## 4. 平台基建（四助手共享）

### 4.1 Host 写（P0 阻塞文件 / 相册落盘）

今日：`hostaccess` 注释即 read-only；`authorize` 只按 read 校验。

需要：

| 工具 / API | 行为 | 权限 |
|------------|------|------|
| `HostRename` / `HostMove` | 同挂载点优先 rename；跨目录 move | grant `readwrite` |
| `HostCopy` | 复制文件或目录树 | 源 read，目标 readwrite |
| `HostMkdir` | 建目录 | readwrite |
| `HostDelete` | 删文件/空目录；非空目录需显式 `recursive` | readwrite + 默认 AskUser / automode 高风险 |

约束：

- 路径解析继续防 `..` 逃逸；符号链接策略与现读路径一致（不跟出 grant）。
- 单次工具调用限制条目数（如 ≤ 100）；更大批量走「计划文件 + 确认执行」两阶段。
- 控制台「可见范围」里 `读写` 文案与真实能力对齐（今日勾了读写却不能写是债）。

### 4.2 变更计划（BatchPlan）

统一结构（可先 JSON 落在会话 / 事项附件，不必先做独立表）：

```text
BatchPlan
  id, assistant_id, kind: rename|move|dedup|organize|download
  items[]: { op, from?, to?, url_or_magnet?, save_path?, note }
  status: draft|approved|running|done|cancelled
```

交互：Agent 生成 draft → 用户在对话或协助单里批准 → 工具 `ApplyBatchPlan` 执行并回写结果。

### 4.3 下载连接器（媒资核心）

```text
DownloadBackend
  id, type: qbittorrent|aria2|fnos_download|custom_webhook
  base_url / 凭据（进设置密文）
  default_save_path（须落在某助手 grant 内，提交时再校验）
```

Agent 工具（示意）：

- `ListDownloadBackends`
- `SubmitDownload`（url / magnet / torrent 文件路径 + save_path + 可选 category）
- `ListDownloadTasks` / `GetDownloadTask`（进度摘要，供补全与追问）

流程：

```text
用户诉求（缺一季 / 某张专辑）
  → 助手用 WebSearch + Browser 找候选源
  → 整理候选表（标题、清晰度/码率、体积、链接类型、备注）
  → 用户点选或口头确认
  → SubmitDownload 到已配置后端
  → （可选）完成后 HostRename / 入库整理
  → （顺带）返回「可播放路径」或系统打开深链
```

**明确不做（本期）：**

- Roundpen 进程内 HTTP 直下整片到内存再写盘（大文件、易超时、绕过用户下载器配额与限速）。
- 内置 BT 客户端。
- 产品内写死第三方影视/音乐盗版站列表或「一键破解」流程。

### 4.4 元数据与「补全」（媒资）

「补全」优先指**库内缺口**，其次才是刮削字段：

1. **缺口扫描**：按用户约定的目录规范（如 `Movie/Name (Year)/`、`TV/Show/SxxExx`、`Music/Artist/Album/`）扫 grants，列出缺失集数 / 曲目。
2. **搜源**：对缺口条目发起联网搜索与浏览器辅助（登录态站点依赖用户自己的 Browser 会话，与 routines 设计一致：缺登录则降级说明）。
3. **元数据（可选 P1.5）**：TMDB / TVDB / MusicBrainz 等公开 API 做识别与命名建议；海报 NFO 非必须。

播放 / 打开：Agent 工具 `OpenHostPath`（action=`open`|`details`|`manager`）→ 控制台在飞牛桌面调用 Trim JS SDK（`openFile` / `showFileDetails` / `openFileManager`）。飞牛开放文档目前**无**下载中心后端 API，投递下载仍走用户配置的 qBittorrent / webhook。

---

## 5. 分助手范围

### 5.1 相册助手（P0）

**用户故事**

- 「看看去年的今日 / 五年前的今日」
- 「把这些放进『2026 回忆』目录」
- 早间 digest：今日回忆摘要（升级现有 OnThisDay）

**能力分期**

| 阶段 | 内容 |
|------|------|
| P0a | 对话触发扫描（复用/抽出 OnThisDay）；`ReadImage` 抽选展示；只读 |
| P0b | 轻量索引：路径 + 拍摄日（EXIF DateTimeOriginal，失败则文件名/mtime）；按日查询 O(命中) 而非每次全盘 Walk |
| P0c | 确认后 `HostCopy` / `HostMkdir` 进目标回忆目录；可选 routine「每天 8:00 推今日回忆」 |

不做：完整人脸相册产品、替代飞牛智能相册。

### 5.2 文件助手（P1，依赖 host 写）

**用户故事**

- 「把下载目录里乱七八糟的文件按年份归位」
- 「批量去掉文件名前缀」「找出重复照片/安装包」

**能力**

- 规则重命名（模板：`{date}_{name}` 等）→ BatchPlan
- 查重：尺寸 + hash（xxhash/sha256）；同 hash 给保留建议，删除须确认
- 目录重规划：先出树形搬迁计划，再 Apply

Shell：仅在用户打开能力且路径在 grant 内时用于加速哈希；优先提供专用工具，避免「任意 Bash 管 NAS」。

### 5.3 影视助手（P1 基建 + P2 体验）

**用户故事**

- 「我缺《某某》第 3 季，帮我找源并丢进下载器」
- 「扫一下片库，列出缺集」
- 「下完按规范改名」

**能力优先级**

1. 下载后端连接 + Submit/List  
2. Browser + Search 找源 → 候选表 → 确认投递  
3. 片库缺口扫描 + 命名整理（host 写）  
4. 播放深链（顺带）

### 5.4 音乐助手（与影视同构）

同一套下载与 BatchPlan；差异在：目录规范、搜索话术、元数据源（音质 / 曲目列表）。可共享同一 `DownloadBackend` 配置。

---

## 6. 安全、权限与合规

| 风险 | 对策 |
|------|------|
| Agent 误删 NAS 数据 | Delete / 递归操作默认确认；BatchPlan；grant 外不可达 |
| 读写 grant 名不副实 | 实现写工具时同步 UI/文案与 policy |
| 下载路径逃逸 | `save_path` 必须通过助手 directory grant 的 readwrite 校验 |
| 凭据 | 下载器 API Key / 密码进设置密文存储，只注入控制面，不进模型上下文明文 |
| 来源合法性 | 产品说明：Roundpen 提供自动化能力，不提供版权内容；用户自备来源与索引；不内置侵权站点导航 |

Automode：host 写与 `SubmitDownload` 归入高风险类；可对「仅投递到已允许后端 + 已允许目录」配受信规则，但首发默认询问。

---

## 7. 里程碑

| 里程碑 | 交付 | 验收 |
|--------|------|------|
| **M0** Host 写 + BatchPlan 最小集 | Rename/Move/Copy/Mkdir/Delete + draft/approve/apply | 文件助手能在授权目录完成一次确认后的批量重命名 |
| **M1** 相册 P0 | 索引 + 对话往年今日 + 可选 copy 到回忆目录 + routine 钩子 | 「去年的今日」有图可点；可拷到目标文件夹 |
| **M2** 场景包 | 四助手模板 + `EnsureNASScenePack`（fnOS 默认开）+ 设置「家庭 NAS 助手」 | fnOS 首登侧边有四助手；非 fnOS 设置里可手动开 |
| **M3** 下载连接器 | ≥1 个后端（建议 qBittorrent Web API）+ Submit/List | 对话确认后任务出现在下载器且路径在 grant 内 |
| **M4** 影视/音乐搜源闭环 | 缺口扫描 + Browser/Search 候选 + 投递 + 可选整理 | 「缺集补全」从扫库到入队可演示；播放非必须 |
| **M5** fnOS 对齐（可选） | 下载中心 / 打开文件等官方 API | 飞牛用户少配第三方下载器 |

建议实现顺序：**M0 → M1 → M2 → M3 → M4**，M5 视飞牛 API 成熟度插入 M3 之后。

---

## 8. 开放项（实现前拍板）

1. **影视 / 音乐**：保持两个助手，还是一个「媒资助手」两个技能？→ 默认两个，共享下载器配置。  
2. **场景包默认 / 入口**：~~原开放~~ → **已定**：`authProvider=fnos` 默认开；管理入口在 **个人设置 → 工作环境 → 家庭 NAS 助手**；日常使用在助手侧边点开。  
3. **索引存储**：相册/片库索引放用户 DB 表 vs 工作区文件？→ 倾向用户 DB（可被多会话查询）；大盘扫描要限流与增量。  
4. **第一个下载后端**：qBittorrent vs aria2 vs 只做 webhook？→ 建议 **qBittorrent Web API**（家用 NAS 常见）+ 通用 webhook 保底。

---

## 9. 非目标（再次强调）

- 自研影音播放器或转码集群  
- 替代飞牛相册 / Jellyfin 的完整媒体服务器  
- 内置商业片源或破解站导航  
- 无确认的全自动删盘 / 全自动付费下载  

---

## 10. 下一步

1. 本设计评审通过后写实现计划：`docs/superpowers/plans/2026-11-05-nas-builtin-agents.md`（按 M0–M4 拆任务）。  
2. M0 可独立开 PR（host 写），不阻塞场景包文案。  
3. 媒资搜源话术与技能在 M3 前用 fixture / 假下载后端联调，避免卡在真实站点波动。
