# 助手创建向导：LLM 门禁 + 自动环境准备

日期：2026-09-12  
状态：已对齐，待实现计划  
范围：新建助手向导的门禁、幕后规划、白名单代跑与进度 UI（不含聊天式配置、不含任意 shell）

关联：`docs/superpowers/specs/2026-09-12-assistant-first-ui-design.md`（助手优先 IA；本文件加重「创建」中的环境就绪段）

## 1. 目标

让用户在**不理解 Sandbox / QEMU / qcow2** 的前提下，第一次就能把助手建起来并真正能干活。

- **先能说话**：未配置 LLM 时引导接通大脑，再进入创建。
- **不问用户聊环境**：创建过程中不出现与 LLM 的对话窗。
- **自动准备工位**：根据 Probe 与用户已填选项，幕后拼提示词 → 出白名单安装计划 → 平台执行。
- **敏感操作需确认**：如 `apt` / sudo；纯镜像构建可自动开跑。
- **过程可见**：用户看到在装什么；可展开看命令、日志、耗时。

## 2. 原则

| 原则 | 含义 |
|------|------|
| 助手优先文案 | 用「接通大脑」「准备工位」「浏览器画面」；不对普通用户暴露 Sandbox / 槽位 / qcow2 |
| 无创建聊天 | LLM 只做结构化规划，不参与向导对话 |
| 白名单执行 | 仅允许预置 `SetupAction`；禁止模型输出任意 shell |
| 人确认高风险 | 改系统包管理器 / 需提权的动作必须确认 |
| 可观测 | 每项动作有状态；展开可见细节 |
| 可恢复 | 失败可重试该项；不丢已填名称 / 身份 / 能力 |

## 3. 产品决策摘要

| 主题 | 决策 |
|------|------|
| 向导形态 | 加长表单向导 + 自动「准备工位」步（非对话式） |
| LLM 角色 | 门禁依赖 + 幕后规划器（固定模板提示 + Probe JSON → 计划 JSON） |
| 执行方 | 平台代跑白名单动作 |
| 敏感操作 | 用户确认后继续；可提供计划内降级项（若有） |
| 镜像构建 | 默认自动执行，进度可见 |
| 与旧三问关系 | 称呼 / 身份 / 能力意图保留；中间插入门禁与准备工位 |
| Admin | 设置里 LLM gateway + Runtime 面板仍可用；向导不替代运维面板 |

## 4. 向导步骤

### 4.1 总览

1. **接通大脑**（条件步）— LLM gateway 未就绪则展示；已就绪跳过。  
2. **怎么称呼它** — 名称（必填）+ 简介（强烈建议）。  
3. **以谁的名义** — 代理我 / 独立身份。  
4. **能力意图** — 预设 + 网络等表单（非聊天）。  
5. **准备工位** — 进入即自动：探测 → 规划 → 执行；列表 + 可展开细节。  
6. **完成** — 落助手档案 → ensure session → 进入对话。

步骤可前后返回（准备工位进行中时返回需提示：进行中任务继续或取消策略见 §6.4）。

### 4.2 接通大脑

**就绪条件**（实现时以现有 settings / LLMGW 为准，需同时满足）：

- LLM gateway 启用；
- 至少一条可用上游（OpenAI-compatible 或 Anthropic）配置了 base URL + API key；
- 默认模型非空（或平台有明确回退且探测通过）。

未就绪 UI：

- 人话说明：助手需要模型才能规划工位并后续对话。  
- 嵌入或深链到现有 Settings「LLM gateway」关键字段；保存后向导重新探测。  
- Admin 与非 admin：非 admin 若无权改系统设置，展示「请联系管理员配置模型」并阻断创建（本期不引入用户级自备 key，除非后续单开规格）。

### 4.3 称呼 / 身份 / 能力意图

与助手优先设计一致：

- 名称、简介、身份二选一、能力预设（写作 / 代码 / 代码+浏览器…）、网络档。  
- **不**让用户选择引擎、镜像路径、QEMU。

能力意图进入规划输入：例如需要浏览器画面 → 可能触发 `build_browser_image`；仅写作文件 → 仍可能需要 agent 盘与 QEMU（平台默认工位），但规划器可据 Probe「已就绪则空计划」。

### 4.4 准备工位（核心）

进入本步后**立即**开始，无需用户再点「开始检测」（可保留「重新检测」）。

流水线：

1. **Probe** — 复用 `runtime.Probe`（二进制、agent/browser qcow2 等）。  
2. **Plan** — 服务端用已配置 LLM + 固定系统提示 + 结构化用户上下文 + Probe JSON，要求模型只输出计划 schema（见 §5）。  
3. **Reconcile** — 服务端校验：动作 ∈ 白名单；去掉已就绪项；敏感标记以服务端表为准（模型不可降级敏感级）。  
4. **Execute** — 非敏感且未完成的动作自动排队执行；敏感动作 `awaiting_confirmation` 直到用户确认。  
5. **Gate** — 计划全部 `succeeded` 或用户明确接受「降级完成」后，才允许点「创建助手」。若仍缺硬依赖则禁用完成。

#### UI

- 顶部一句总结（人话，可来自计划的 `summary` 字段，经服务端长度与敏感词裁剪）。  
- 动作列表，每项：  
  - 标题（人话，如「安装本机虚拟机组件」）  
  - 状态：等待确认 / 排队 / 进行中 / 完成 / 失败 / 已跳过  
  - 主操作：确认、重试（失败时）  
  - **展开**：命令文案、最近日志、开始/结束时间、错误信息  
- 全局进度（完成数 / 总数）与可选折叠「全部日志」。  
- 文案避免暴露内部路径；展开区允许显示真实命令（运维可核对）。

## 5. 规划与执行模型

### 5.1 计划 schema（概念）

```json
{
  "summary": "需要本机虚拟机组件和助手系统盘",
  "actions": [
    {
      "id": "install_qemu",
      "title": "安装本机虚拟机组件",
      "reason": "当前环境缺少 qemu-system-x86_64"
    },
    {
      "id": "build_agent_image",
      "title": "准备助手系统盘",
      "reason": "默认工位镜像尚未构建"
    }
  ]
}
```

- `id` 必须落在服务端白名单。  
- 模型**不**提供 raw shell；命令与实现由平台绑定。  
- 未知 `id` → 丢弃并记日志；若丢弃后无法满足硬依赖 → 计划失败，向用户说明并允许重试规划。

### 5.2 白名单动作（本期）

| Action ID | 人话标题（默认） | 敏感 | 实现意图 |
|-----------|------------------|------|----------|
| `install_qemu` | 安装本机虚拟机组件 | 是（须确认；能否代跑见 §5.3） | 等价于探测里的 install 命令（如 `apt install -y qemu-system-x86 qemu-utils`）；root/免密 sudo 则确认后代跑，否则复制命令自助 |
| `build_agent_image` | 准备助手系统盘 | 否 | `make agent-image` / `images/agent-qemu/build.sh` |
| `build_browser_image` | 准备浏览器画面环境 | 否 | `make browser-image` / `images/browser-qemu/build.sh` |

后续可扩展（非本期必做）：`ensure_docker`、`build_code_agent_image` 等，仍须进白名单表。

硬依赖规则（服务端）：

- 默认引擎为 QEMU 时：agent 工位需要 binaries + agent image。  
- 能力含浏览器：额外 browser image。  
- Probe 已就绪的对应项不得进入待执行队列。

### 5.3 敏感执行策略（sudo 探测）

- **确认**：凡敏感动作仍须用户点「允许并继续」；不会在未确认时静默 `apt`。  
- **禁止**：Web/API 收集 sudo 密码；密码不进模型、不进日志。  
- **探测**（进入准备工位或执行 `install_qemu` 前，服务端对**运行 roundpend 的同一用户**检测）：  
  1. 已是 root（`euid == 0`）→ `privilege: auto`  
  2. 否则若存在 `sudo` 且 `sudo -n true` 成功（免密）→ `privilege: auto`  
  3. 否则 → `privilege: manual`（无法非交互提权）  
  探测结果写入该动作的元数据，UI 据此切换交互，不把内部判定词暴露成吓人错误。  
- **`privilege: auto`**：用户确认后平台直接执行白名单安装命令（root 下无 sudo 前缀；免密则 `sudo -n …`）。成功后自动 Probe。  
- **`privilege: manual`**：不尝试弹密码或 `sudo -S`。UI 展示：  
  - 人话说明「需要在本机终端安装虚拟机组件」；  
  - 可一键复制的完整命令（与 Probe SetupStep 一致，如 `sudo apt install -y qemu-system-x86 qemu-utils`）；  
  - 主按钮「我已装好，重新检测」→ 再 Probe；通过则该项 `succeeded`，失败则保留说明可重试。  
- **可选文档**（非向导强制）：运维可自配窄权限 NOPASSWD，使本机变为 `auto`；向导不自动改 sudoers。

### 5.4 并发与幂等

- 按 action id 全局（或 per-host）锁：同一 `build_agent_image` 不并行两次。  
- 已成功的产物：再次 Probe 通过则跳过。  
- 构建中途进程崩溃：下次进入向导重新 Probe；不完整产物由现有 `BUILD_INCOMPLETE` / 尺寸检查拒绝并允许重建。

### 5.5 LLM 提示拼装（服务端）

输入仅包括：

- 固定系统说明（只许输出 schema、只许白名单 id、用中文 summary/title/reason）；  
- 用户上下文：名称、简介、身份、能力预设、网络档；  
- Probe 快照：missing、setup 建议、engines ready 标志；  
- 白名单目录（id + 说明 + 何时需要）。

输出：严格 JSON；解析失败则重试一次或回退**确定性规则规划**（无 LLM）：按 Probe.Setup 映射到白名单动作——保证 gateway 抖动时向导仍可用。

## 6. API / 状态（概念）

不必在本规格锁死路径名；实现计划再定。概念资源：

| 概念 | 作用 |
|------|------|
| `WizardDraft` | 保存步骤 2–4 的表单（可选 localStorage + 服务端） |
| `SetupPlan` | 一次规划结果 + summary + actions |
| `SetupActionRun` | 单动作状态、日志环形缓冲、确认时间 |
| `confirm` / `retry` / `reprobe` | 用户操作 |

准备工位页面对 `SetupActionRun` 做轮询或 SSE/WS；日志以追加片段返回。

### 6.4 离开与取消

- 离开准备工位：后台构建可继续（镜像构建）；敏感未确认项保持等待。  
- 「取消准备」：取消排队中任务；进行中的 build 尽量取消（best-effort）；已安装的 apt 包不回滚。

## 7. 与现有组件关系

| 现有 | 用法 |
|------|------|
| `internal/runtime.Probe` / `NotReady` / SetupStep | 探测与命令文案来源 |
| Settings LLM gateway | 「接通大脑」配置面 |
| `images/*/build.sh`、Makefile targets | `build_*_image` 实现 |
| `AssistantCreatePage` 三步 | 扩展为完整向导；准备工位为新步 |
| RuntimePanel | Admin/设置保留；向导面向最终用户 |

创建完成仍走现有 `assistantsApi.create` + `ensureSession`；**仅当**准备工位门禁通过（或引擎已就绪空计划）后调用，避免再现「provision sandbox: qcow2 missing」。

## 8. 非目标

- 创建过程中与 LLM 多轮对话配置环境。  
- 模型生成并执行任意 shell。  
- 本期自动配置 passwordless sudo（可文档化，不强制安装器改 sudoers）。  
- 手机 / 桌面能力真装。  
- 多机 / 远程 host provisioning。  
- 替换 Settings 里的 Runtime 运维面板。

## 9. 验收标准

1. 未配 LLM 时，新建助手先进入接通大脑；配好后可继续。  
2. 向导中无聊天输入框与助手消息流。  
3. 缺 qemu 二进制或 agent.qcow2 时，准备工位自动列出对应动作并执行（敏感项先确认）。  
4. 用户可见动作列表；展开可见命令或构建日志。  
5. 全部必需动作成功后，创建助手不再因缺 `agent.qcow2` 失败。  
6. LLM 规划失败时，确定性回退仍能根据 Probe 生成等价计划。  
7. 文案主界面不出现 Sandbox / qcow2（展开细节允许技术命令）。  
8. root 或 `sudo -n` 可用时：确认后自动安装 qemu 包；否则只给复制命令 +「我已装好，重新检测」，且从不向用户要 sudo 密码。

## 10. 开放实现细节（留给实现计划）

- 日志推送用轮询还是 WS。  
- WizardDraft 是否必须服务端持久化。  
- 规划调用走 LLMGW 的哪条内部 API / 虚拟 key。  
- `sudo -n` 探测是否缓存 TTL（避免每次列表刷新都打 sudo）。
