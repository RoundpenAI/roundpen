# Agent=Docker、账号工作区与镜像分发

日期：2026-09-12  
状态：已对齐，待实现计划  
范围：槽位后端钉死、一级导航「工作区」、Agent OCI 分发与容器命名；不含 Browser qcow2 发版细节与视觉稿

相关：`2026-09-12-assistant-first-ui-design.md`（助手优先 IA）、`docs/architecture/environment-services.md`、`docs/architecture/qemu-browser.md`

## 1. 目标与背景

助手优先之后，旧 Workbench（`/s/:id` + FileTree）不再进主导航，「给 Agent 投放资料 / 管工作区文件」没有入口。同时 Agent 曾尝试统一 QEMU（`workspace.qcow2`），导致：

- 宿主机 `workspace.FS` 与 guest 真实工作区不是同一份盘
- 「host 直管文件」与 UID 权限、停机挂载纠缠在一起
- 设置里还要选 Agent 运行时（QEMU / Docker / Kern），与「用户不选引擎」冲突

本设计收束为：**Agent 固定 Docker + 官方 OCI pull；Surface 固定 QEMU；账号级工作区页经容器读写文件。**

## 2. 决策摘要

| 主题 | 决策 |
|------|------|
| Agent 后端 | **仅 Docker**；部署硬依赖 Docker |
| Browser / Desktop / Mobile | **仅 QEMU** |
| Kern | **删除**（不再作为运行后端） |
| Agent-on-QEMU | **不做默认**；默认路径与内置模板收敛掉 |
| 设置「Agent 运行时」选择 | **去掉** |
| 工作区入口 | 一级导航 **工作区** → `/workspace`（账号级，多助手共享） |
| MVP 文件操作 | 浏览 / 上传 / 下载 / 删除（不做在线编辑） |
| 文件权限路径 | **经已 Ensure 的 Agent 容器**读写（不默认 host 直管写；不做 idmapped 默认） |
| Agent 容器名 | 稳定 **`roundpen-{user}`**（username 规范化） |
| 默认 Agent 镜像 | **官方注册表 pull**（建议 GHCR 与发版同源）；离线 `docker load` |
| 终端用户本机 `docker build` | **不是**主路径；留给开发者与深度定制 |

## 3. 槽位与后端分工

```
助手（策略 / 对话）
  └── 能力开关 → 使用账号级 Surface / 工作区
        ├── Agent 槽位：Docker 容器 roundpen-{user}
        │     └── /workspace ← 资料根（账号共享）
        ├── Browser / Desktop：QEMU（画面 + CDP）
        └── Mobile：预留 QEMU
```

| 槽位 | 后端 | 与 `/workspace` |
|------|------|-----------------|
| Agent | Docker | **主场**；资料与工具高频交互 |
| Browser / Desktop / Mobile | QEMU | **默认不挂整棵工作区**；日后若需要仅窄共享子目录（inbox/downloads） |

说明：助手详情「可见范围」继续管**本机目录授权**；默认资料根的浏览/投放在「工作区」页，不在助手详情重做一套文件树。

## 4. 工作区产品与 API

### 4.1 信息架构

- 一级菜单：助手 | **工作区** | 设置（| admin 镜像）
- 路由：`/workspace`
- 页内：路径面包屑 + 列表；上传 / 下载 / 删除；空态说明所有助手可共用此资料根
- 打开页面或调用文件 API 时：若 Agent 未运行则 Ensure；UI「正在准备工作区…」
- 失败：明确「Docker 不可用」或「默认 Agent 镜像无法拉取」，不静默落到错误的 host 目录视图

### 4.2 用户可见 API

| 方法 | 路径 | 行为 |
|------|------|------|
| GET | `/v1/me/workspace/files?path=` | list |
| GET | `/v1/me/workspace/files/content?path=` | download |
| POST | `/v1/me/workspace/files?path=` | upload（body 流） |
| DELETE | `/v1/me/workspace/files?path=` | delete |

实现顺序：鉴权（属主）→ `EnsureAgent(user)` → 在该用户 Agent 容器内、以容器运行用户操作，路径限制在 `/workspace`，拒绝 `..` 逃逸。

容器内 IO 优先复用现有 Exec / 拷贝通道（例如 `docker exec` 做 list/rm，`docker cp` 或等价流式拷贝做 up/download），**不**再默认走宿主机 `workspace.FS` 写路径；具体封装落在实现计划。

旧 `/v1/sandboxes/{id}/files`：可暂留 admin/兼容；**工作区页只走 `/v1/me/workspace/*`**，不向普通用户暴露 sandbox id。

### 4.3 权限（为何不 host 直管）

Docker bind 下 host UID 与 guest UID 不一致会导致上传与 Agent 工具互相踩权限。备选曾评估：

- PUID/PGID 合同：部署约定重，易乱
- idmapped mount（`match-user`）：内核 ≥ 5.12、runc ≥ 1.2、FS 须支持 idmap；Docker Engine 友好 API（`BindOptions.IDMapping` / API v1.56）尚新，且 **rootful only**，NFS/部分 NAS 不友好 — **不做本期默认**
- **已选**：用户侧写路径统一走容器（与 Agent 同一身份）

日后可在探测到 idmap 后增强为真 host 直管；不阻塞本期。

### 4.4 非目标（工作区本期）

- 在线编辑、重命名/移动（mkdir 可作为顺手小项或下一期）
- Browser VM 挂载整棵工作区
- 每助手独立工作区根（仍为账号级共享）

## 5. Agent 容器与镜像

### 5.1 容器约定

- 每登录用户至多一台 Agent 容器，Docker 名：**`roundpen-{normalizedUser}`**
- 控制面内部可保留 sandbox 记录 id；用户 API 与主导航不暴露
- Browser 等 QEMU 实例命名本期不强制；建议日后 `roundpen-{user}-browser` 与 Agent 区分

### 5.2 镜像长什么样（平台默认）

当前配方 `images/code-agent/Dockerfile`（模板 id `code-agent`）方向保留为官方默认：

- 基座：Ubuntu LTS
- 预装：至少 `ca-certificates`、`curl`、`git`、`openssh-client`
- `WORKDIR /workspace`；长驻进程由控制面约定（如 `sleep infinity` + exec）
- 对话 ACP / 模型运行时以平台为准；默认镜像是 **shell + 文件 + git 场地**，不要求镜像内烘焙上游 API key

内置 `base` / `python` / `node` 等仍可作为 admin 模板选项；**`host`（Kern）与默认 `agent-claude`（Agent QEMU）从默认路径移除**。

### 5.3 如何确认用哪张镜像

```
EnsureAgent / 工作区 API
  → 平台默认模板（固定；不再按 engine 分支）
  → template.artifact_ref
  → docker ImageInspect；缺失则 Pull（或文档指引 load）
  → 创建或复用 roundpen-{user}
```

Probe / 设置文案收敛为：Docker daemon 是否就绪、默认 `artifact_ref` 是否本地可用或可 pull——**不再展示 QEMU/Kern/Docker 三选一引擎卡片**。

### 5.4 如何定制

| 角色 | 方式 |
|------|------|
| 平台默认 | CI 构建并推送官方 tag；env 可覆盖默认 `artifact_ref` |
| 私有化换默认 | admin 将「默认 Agent 模板」指到已 ready 的 `slot=agent` 模板 |
| 配方定制 | admin 镜像/模板构建或导入私有 OCI |
| 容器内临时装包 | 助手执行 apt/npm 等（易变）；要固化则重建模板 |

换默认模板后**不**静默重建已有容器；若提供能力，须显式「重建 Agent 环境」（bind 工作区文件应保留）。

### 5.5 分发（单二进制生产关系）

单二进制只交付控制面，**不**假定用户机器上有 git 构建上下文。

| 路径 | 行为 |
|------|------|
| **默认** | 官方注册表 **pull**（建议 `ghcr.io/<org>/code-agent:<semver>`；Docker Hub 可作镜像） |
| **离线** | Release 附 OCI tar + `docker load` 文档 |
| **开发者** | 源码树 `docker build -t …:local` 覆盖 `artifact_ref` |
| **不做主路径** | 单二进制内嵌整盘 OCI；强制终端用户 `docker build`；以 Kaniko/无 Docker 构建当主故事 |

具体 registry org/镜像名在实现计划中与 CI 发版任务一并确定。

## 6. 收敛清单（实现分期建议）

可同一决策、分 PR：

1. **工作区页 + `/v1/me/workspace/*` + Ensure + 容器内文件 IO + 导航**
2. **容器命名 `roundpen-{user}`**（与 Ensure 创建路径对齐）
3. **去掉运行时选择 UI/API；Agent Ensure 固定 Docker + 默认模板**
4. **删除或停用 Kern 后端与相关模板/探测**
5. **Agent-QEMU 默认模板与文档降级/移除；Browser QEMU 保留**
6. **CI 推送官方 code-agent 镜像；默认 artifact_ref 改注册表 tag；Probe pull/load 文案**
7. **README / 架构文档槽位表与安装面（Compose / 单二进制+pull / 离线 load）**

## 7. 风险与开放项

| 项 | 说明 |
|----|------|
| 首次进工作区延迟 | Ensure + 可能 pull；需明确进度与错误 |
| 部署必须有 Docker | 有意简化；无 Docker 环境不再一等公民 |
| 注册表可达性 | 国内/隔离网依赖镜像加速或离线 load |
| 旧 sandbox 文件 API | 兼容期与属主检查保持；避免与 me/workspace 双写语义漂移 |
| idmapped 增强 | 可选后续；需声明本地 FS 与 rootful Docker |
| Browser 窄共享子目录 | 未设计；需要时另开 spec |

## 8. 成功标准

1. 普通用户在一级导航打开「工作区」，无需知道 sandbox id，即可上传资料并在对话中被 Agent 使用。
2. 文件写身份与 Agent 工具一致（经同一容器），不出现「网页上传后 Agent 无法读」的默认失败模式。
3. 新部署：安装 Docker + 拉取/加载官方 Agent 镜像即可 Ensure；**不必**本机持有 Dockerfile。
4. 设置中不再出现 Agent 引擎三选一；文档与代码默认一致：Agent=Docker，Surface=QEMU。
