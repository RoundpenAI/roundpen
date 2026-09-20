# 设置拆分：个人设置与系统管理

日期：2026-09-20  
状态：已实现  
范围：Roundpen Web 控制台的设置信息架构。不含后端 API 变更（`/v1/admin/settings`、`/v1/admin/oauth/providers` 等已有 `RequireAdmin` 门控）。

## 1. 背景

原 `/settings/:section` 把 14 个段落平铺在一条列表里：4 个个人项（git / accounts / password / agent）和 10 个 admin 项（general / oauth / llmgw / webtools / browser / preview / builds / proxy / automode / system）。三个问题：

1. **两种保存模型混在一个页面**：admin 段落共享一个表单 + 底部全局保存栏，且该保存栏对管理员在个人段落（如「密码」）下也渲染；个人面板则是即时生效的独立卡片。
2. **同一领域两种 scope 混排**：`oauth`（平台登录方式）与 `accounts`（个人绑定）、`agent`（个人环境）与 `builds`/`proxy`（平台配置）并列。
3. **导航随角色变形**：非管理员 4 项、管理员 14 项，无分组。

## 2. 目标结构

**个人设置 `/settings/*`**（顶栏「设置」，所有用户）

| 分组 | 段落 |
|------|------|
| 账号与安全 | 关联账号（`accounts`）、密码（`password`） |
| 工作环境 | Git 个人令牌（`git`）、Agent 环境（`agent`） |

默认段落 `accounts`。

**系统管理 `/admin/settings/*`**（顶栏「系统管理」，仅 admin，与 admin-only 的「镜像」同模式）

| 分组 | 段落 |
|------|------|
| 用户与访问 | 通用（`general`）、OAuth 登录（`oauth`） |
| 能力接入 | LLM 网关（`llmgw`）、Web 工具（`webtools`）、浏览器（`browser`）、预览（`preview`） |
| 沙箱与运行时 | 构建（`builds`）、网络代理（`proxy`）、自动模式（`automode`）、系统（`system`） |

默认段落 `general`。

## 3. 路由与权限

- `/settings` → `SettingsLayout area="personal"`；`/admin/settings` → `RequireAdminRoute` 包裹的 `SettingsLayout area="admin"`。
- 布局按 `area` 从 `SETTINGS_AREAS`（`web/src/lib/appNav.ts`）取 `basePath / titleKey / groups / sections / resolve`，段落解析从按位置 `split('/')[2]` 改为按 `basePath` 前缀截取。
- 前端 `RequireAdminRoute` 只做体验（loading 空态、非 admin 重定向到 `/settings`）；服务端 `RequireAdmin` 仍是最终防线。
- 兼容旧书签：管理员访问 `/settings/{admin 段落}` 会重定向到 `/admin/settings/{段落}`；非管理员访问则落回个人默认段落。

## 4. 实现要点

- `web/src/lib/appNav.ts`：`PERSONAL_SETTINGS_SECTIONS` / `ADMIN_SETTINGS_SECTIONS`（带 `group`）、`SETTINGS_GROUPS`、`resolvePersonalSection` / `resolveAdminSection` / `isAdminSectionKey`、`SETTINGS_AREAS`；`PRIMARY_MENUS` 增加 admin 项，`matchPrimaryMenu` 增加 `/admin` 分支。
- `web/src/pages/SettingsLayout.tsx`：改为 `area` prop；桌面侧栏渲染分组标题，移动端 tab 条保持平铺。
- 页面拆分：`web/src/pages/settings/AdminSettingsPage.tsx`（原 `SettingsPage.tsx` 迁移，保留表单 / 保存栏 / AutoMode 弹窗）与 `web/src/pages/settings/PersonalSettingsPage.tsx`（四个个人面板 + 旧路径重定向）。
- i18n：新增 `nav.admin`、`settings.adminTitle`、`settings.group.*`（en 与 zh_CN 同步）。

## 5. 测试

- `web/src/lib/appNav.test.ts`：两个区域的段落顺序/分组完整性、resolver 默认值与越权回退、`isAdminSectionKey`、admin 主菜单可见性与 `matchPrimaryMenu('/admin/...')`。
- e2e URL 迁移到新前缀：`nav.spec.ts`（`/admin/settings/general`）、`settings-automode.spec.ts`、`oauth-login.spec.ts`（OAuth 配置与「配置 OAuth 登录」跳转）；`settings-agent.spec.ts` 与关联账号相关 URL 不变。

## 6. 后续（不在本次范围）

llmgw / webtools / browser 的**用户级覆盖（BYOK）**：形态为「平台默认 + 个人覆盖 + 管理员开关」，个人区将新增「我的服务」分组。调研结论（含存储、relay 归属、CDP 隔离与 SSRF 注意事项）见本仓库对应实现计划。
