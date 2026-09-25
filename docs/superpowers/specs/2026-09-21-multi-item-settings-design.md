# 多 item 设置：列表 + 槽位绑定

日期：2026-09-21  
状态：P1–P6 已实现（通用机制 + proxy/search/browser/llm 四类 kind，旧结构退役）  
范围：`internal/settingitems`（新）、`internal/userenv`、`internal/api/envapi`、`cmd/roundpend`、Web 控制台设置页

## 1. 背景

集成类配置基本都是「全局单值」：LLM 只有硬编码的 openai / anthropic 两个 upstream（`llmgw_upstreams.provider` 是主键，按 `/llmgw/{provider}/` 前缀选路），模型是单个全局 `LlmgwDefaultModel`；web 搜索是一套 endpoint/key/proxy；浏览器是单个 CDP 端点。唯一接近「列表 + 选择」的是 `AppSettings.Proxies` + `users.agent_proxy/browser_proxy`，但它绑死在整体 PUT 的设置文档里（自由文本 id、密钥与非密字段混在一起、每次全量保存）。

需求：把「维护一份条目列表，使用点从中选一个」做成**通用能力**，并让 LLM 支持多 provider：分类器选一个，Agent 的 plan / vision / coding 等模式各选不同 provider + model；同一机制复用到 proxy / search / browser。

## 2. 概念模型

| 概念 | 含义 |
|------|------|
| **kind** | 一类可配置的后端：`llm` / `proxy` / `search` / `browser` |
| **item** | 列表中的一个具名条目，如 llm 的 `openai`、proxy 的 `us-egress` |
| **slot** | 一个使用点，如 `llm.classifier`、`llm.agent`、`proxy.browser` |
| **binding** | 「某 slot 在某 scope 下选了哪个 item」，scope 为 `global` 或 `user:<username>` |

关键性质：**列表是数据、选择也是数据，字段 schema 由 Go 注册表定义并经 `/v1/setting-schema` 暴露**，前端按 schema 渲染表单 —— 新增一类能力只需后端加一个 `KindDef` + 消费侧一行解析，不必再写一套设置表单。

## 3. 存储

两张表（权威 DDL 在 `internal/storage/schema/postgres.sql`，`migrations/0021_setting_items.sql` 仅留档）：

```sql
setting_items    (kind, id) PK, name, description, enabled, position, config JSONB, secrets JSONB, updated_at
setting_bindings (scope, slot) PK, kind, item_id, params JSONB, updated_at
```

- 密钥按字段分别 AES-GCM 密封在 `secrets` 列（`enc:v1:` 前缀，与 settings 文档同一 secretbox），`config` 只存非密字段。
- 不加外键（`item_id=''` 是合法的「继承」态）；引用完整性在 Go 里保证：删除 item 时同事务删除指向它的 binding。
- 数据迁移分两半：settings 文档里的旧字段（providers / proxies / search / CDP）由 `settings.Store.LoadLegacy` 读原始 payload、`settingitems.LegacyImport` 转成 items + bindings，**按 kind 幂等**（该 kind 已有条目即跳过），非 slug 的旧 id 会被规范化并重映射选择；逐用户的代理选择则由 schema 里一段幂等 `DO $$` 从 `users` 列复制进 `setting_bindings` 后再删列。`llmgw_transactions.provider` 是自由文本日志列，取值改为 item id 即可，无需改表。

## 4. 解析

`Catalog` 持有 `atomic.Pointer[Snapshot]`（items + bindings），写入走事务后 `Reload` 换指针；另有 30s 定时 Reload，让共用同一数据库的多个 daemon 互相可见（今天的 relay 是每请求读库，不刷新会是行为回退）。

解析链（每跳都校验 item 存在、`Enabled`、kind 匹配、协议被 slot 允许）：

1. `user:<username>`（仅当 `SlotDef.UserOverride` 且调用方给了用户）
2. `global`（管理员默认）
3. `SlotDef.DefaultItem`
4. 仅 LLM：落到 `llm.default`（只跳一层，不递归）
5. 都不中 → `OK=false`，消费方按「未配置」降级（与今天完全一致）

解析失败一律静默回退，不报错。

## 5. 密钥与响应

- 管理员 GET：secret 字段在 `config` 里替换成掩码（`●●●●●●●●`），`secrets` 列永不下发；管理员 PUT 提交掩码或空串 = 保留原值。**行为变化**：显式空串不再清除密钥（旧 `webSearchApiKey:""` 会清），清除改为禁用或删除条目。
- 用户 GET：只返回 `KindDef.Selectable` 声明的字段（proxy 为空集，URL 绝不外泄），沿用 `proxyView` 的既有规则。

## 6. API

```
GET    /v1/setting-schema                      任意登录用户；kinds + slots 定义
GET    /v1/admin/setting-items?kind=           管理员列表（掩码）
POST   /v1/admin/setting-items                 创建
PUT    /v1/admin/setting-items/{kind}/{id}     保存（掩码=保留）
DELETE /v1/admin/setting-items/{kind}/{id}     被引用时 409 {"boundSlots":[...]}，?force=true 级联
PUT    /v1/admin/setting-items/order           排序
GET    /v1/admin/setting-bindings              各 slot 的全局默认
PUT/DELETE /v1/admin/setting-bindings/{slot}
GET    /v1/me/setting-items?kind=              可选条目（仅 selectable 字段）
GET    /v1/me/setting-bindings                 个人覆盖 + 生效值（effective/source）
PUT/DELETE /v1/me/setting-bindings/{slot}      个人覆盖；RebuildsEnv 的 slot 回带重建结果
```

`GET/PUT /v1/me/proxies|proxy` 已移除（`envapi`），由上面的通用端点取代；移动端不读这些接口。

## 7. 落地内容（P1：机制 + proxy）

- 新包 `internal/settingitems`：`Item`/`KindDef`/`SlotDef`/`Binding`/`Registry`、`Catalog`+`Snapshot`+`Resolve`、`Store` 接口与 PG / 内存两种实现（内存实现供单测与 uismoke 桩复用）、`LegacyImport`。
- kind 定义放在消费方包：`userenv.ProxyKind()` / `userenv.ProxySlots()`（`proxy.agent`、`proxy.browser`，均可被用户覆盖且需要重建环境）。
- 运行时接线：`envSvc.Proxy`、`agentapi` 的会话代理、env 重建（`settingitems.EnvRebuilder` 由 `cmd/roundpend` 适配 `RecreateAgent/RecreateBrowser`）全部改走 catalog。
- 前端：`api/settingItems.ts`（含 `slotChoices` 薄封装）、`pages/settings/items/ItemEditor.tsx`（按 Field 类型渲染）、`ItemListSection.tsx`（列表 + 增删改 + 单条保存）；`ProxySection` 改写为 `kind="proxy"`；`BrowserPage` / `AgentEnvironmentPanel` 的选择器换成通用端点。
- 测试：`internal/settingitems` 单测（解析链、绑定校验、删除保护、密钥掩码、legacy 导入、slug 重映射）+ PG 测试（密文落库、跨 catalog 可见、级联删除）+ `web/e2e/settings-proxy.spec.ts`（管理端增删改与掩码往返）。

## 8. 后续阶段

| 阶段 | 内容 | 验收 |
|------|------|------|
| P2 | search kind：`WebSearch*` 迁移、`SysDeps.WebSearch` → `func(userID)` | 密钥不回显；解析不到时搜索工具不注册 |
| P3 | browser kind：`CDP*` 迁移、`Hub.SetProfileResolver`、`RequireBrowser(profile)`、live-link 走解析 | 两个 browser item 并存；用户覆盖真的改变 attach 目标 |
| P4a | LLM item + relay：`/llmgw/{itemID}/` 单路由（种子 id 保持 `openai`/`anthropic`）、`handleSetup` 走快照、`Embed` 走 `llm.embedding` | 种子 id 下 relay 行为不变；新增第三个 provider 可直连 |
| P4b | LLM 槽位接线：分类器、系统 agent、hostsetup planner、沙箱 env | 分类器绑到第二个 provider 后流量出现在 `/v1/llmgw/logs` |
| P5 | 槽位 UI（管理端默认 + 个人覆盖）与 `llm.plan/vision/coding` 模式槽位投影到 env | 改 `llm.plan` 改变沙箱 env；跨 item 时只导出 `ROUNDPEN_LLM_PLAN_*` |
| P6 | 退役：删 `users.agent_proxy/browser_proxy` 列、`DROP TABLE llmgw_upstreams`、清理 `AppSettings` 旧字段与 decode 分支 | 从旧库 dump 迁移后全测试绿 |

## 9. 实现说明（与原计划的差异）

- 槽位的**全局默认**选择器（`SlotBindings`）随 P2 提前落地：每种 kind 的 items 列表下面直接就是它的槽位选择，管理员不用等到 P5 才能让某个后端生效。条目本身在列表里只显示名称/id/状态/操作，新增与编辑放在浮层（`ItemEditorModal`）里，避免多字段表单铺满页面。
- 个人覆盖在个人设置新增的「我的服务」分区（`/settings/slots`），按槽位列出可覆盖项，沙箱类槽位保存后回带重建结果。
- `llm.classifier` 的模型折进了绑定参数，`autoMode.model` 字段随之删除（automode 页面不再有模型输入）。
- 显示名称可选：`NormalizeItem` 在 name 为空时用规范化后的 id 兜底（旧数据里 name 为空的行因此由「跳过」变为可导入），前端不再拦空名，列表在 name 等于 id 时不再重复渲染 id。
- 枚举字段的选项文案走 i18n：`settings.itemOption[.kind].<字段>.<值>` 给下拉标签、`settings.itemOptionHint.<kind>.<字段>.<值>` 给选中后的说明（缺失时分别回落到原始值与字段 hint）。browser 的 provider 五档即用它写清「这个来源要填什么、有什么前提」。多行说明用 `\n`，`Field` 的 hint 按 `pre-line` 渲染。
- 旧库迁移不读 `users` 列：schema 里用一段幂等 `DO $$` 把 `agent_proxy`/`browser_proxy` 复制进 `setting_bindings` 后再删列，同时 `DROP TABLE llmgw_upstreams`。settings 文档里退休的字段由 `settings.Store.LoadLegacy` 读原始 payload 读取，环境变量作为新装种子（`settings.FromConfigLegacy`）。

## 10. 已知限制

- `ANTHROPIC_BASE_URL` 是单值：模式槽位绑定到与 `llm.agent` 不同的 item 时无法靠 Claude Code 路由，只导出 `ROUNDPEN_LLM_<MODE>_*` 供未来消费方使用（P5）。
- embedding 维度固定 `VECTOR(1024)`，绑到不同维度的模型会静默污染插入，需在槽位文案中提示。
- 快照是进程内的，多 daemon 共库靠 30s 定时刷新收敛。
