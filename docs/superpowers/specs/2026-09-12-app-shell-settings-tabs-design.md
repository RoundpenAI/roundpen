# 统一应用壳与设置分段导航设计

日期：2026-09-12  
状态：已对齐，待实现计划  
范围：Roundpen Web 控制台的应用壳信息架构；设置页分段；镜像页接入壳。不含后端 API 变更与镜像二级栏。

## 1. 目标

把「助手 / 设置 / 镜像」统一成同一套左右应用壳，替代设置与镜像当前的 `PageShell` 顶栏布局；并把设置里原先独立的 Agent runtime、Git personal tokens 收进与其它设置项一致的分段导航。

**成功标准**

- 三大顶级菜单共用同一壳；一级轨可折叠为窄图标轨，折叠状态可持久化。
- 助手：一级 + 二级（助手列表）+ 内容（对话 / 详情等）。
- 设置：一级 + 二级（设置段落）+ 内容（对应表单 / 面板）。
- 镜像：一级 + 内容（暂无二级栏）；内容仍为现有镜像列表与对话框。
- 非 admin 在设置中仍能看到 Runtime 与 Git tokens；admin 额外看到系统设置段落。

## 2. 已选方案

**抽公共 `AppShell`（方案 1）**：一级导航轨、可选二级栏插槽、内容区统一由壳提供；助手 / 设置 / 镜像作为模块填入二级栏与内容。不扩展现有 `AssistantLayout` 硬塞其它模块，也不三页各自复制壳。

## 3. 信息架构

### 3.1 三栏结构

| 区域 | 行为 |
|------|------|
| **一级轨** | 顶级菜单：助手、设置、镜像。可折叠为窄图标轨（仅图标）；折叠状态写入 `localStorage`。 |
| **二级栏** | 按模块可选：助手 = 助手列表；设置 = 设置段落；镜像 = 本期不渲染。 |
| **内容区** | React Router `Outlet`（或模块根下的子路由）。 |

移动端：一级 / 二级以 SideSheet 或抽屉呈现，行为与现有助手布局的移动端菜单对齐。

### 3.2 顶级菜单

| 菜单 | 路径前缀 | 二级栏 | 可见性 |
|------|----------|--------|--------|
| 助手 | `/a` | 助手列表（现有逻辑） | 登录用户 |
| 设置 | `/settings` | 设置段落列表 | 登录用户 |
| 镜像 | `/registry` | 无 | admin |

「浏览器」等 advanced 入口本期不进入一级轨；现有 `/browser` 等路由保持可用，不挡在壳的必经路上。

### 3.3 路由

- `/a…`：助手模块（二级 = 列表；内容 = 现有 create / detail / chat / session）。
- `/settings` 与 `/settings/:section`：设置模块；`:section` 记住当前段落，刷新可还原（不使用 query）。
- `/registry`：镜像模块；仅一级 + 内容。

设置与镜像不再包在 `PageShell` 内。`PageShell` 若无其它引用可删除，或暂留并标明废弃。

## 4. 设置模块

### 4.1 二级段落顺序

| key | 标签 | 可见性 | 右侧内容 |
|-----|------|--------|----------|
| `runtime` | Agent runtime | 所有人 | 现有 `RuntimePanel` |
| `git` | Git personal tokens | 所有人 | 现有 `GitCredentialsPanel` |
| `general` | General | admin | 现有 General 表单 |
| `preview` | Preview | admin | 现有 Preview 表单 |
| `builds` | Builds | admin | 现有 Builds 表单 |
| `browser` | Browser | admin | 现有 Browser 表单 |
| `llmgw` | LLM gateway | admin | 现有 LLM gateway 表单 |
| `system` | System | admin | 现有 System 只读信息 |

默认段落：非 admin → `runtime`；admin → `runtime`（或首个可见项）。非法 / 无权限的 `:section` 重定向到默认段落。

### 4.2 交互与保存

- 点击二级项只切换右侧内容；去掉设置页内水平 `Tabs`。
- Runtime / Git 仍各自即时保存，**不**进入全局 Save。
- Admin 系统表单保留底部固定 Save / Reload；仅当表单脏时 Save 可点；文案与校验逻辑不变。
- 切换段落不丢失未保存的 admin 表单脏状态（同页状态保留，或明确提示——实现时优先「同页切换保留 dirty」）。

## 5. 组件边界

### 5.1 新增

- `AppShell`：一级轨（折叠、图标、当前选中、admin 过滤）、二级栏插槽、内容区、顶栏标题；移动端抽屉。
- 小型配置常量：顶级菜单项（路径、图标、admin 标志）；设置段落表（key、标签、admin 标志）。

### 5.2 改动

- `AssistantLayout`：收编为 `AppShell` 下的助手模块提供者（上下文、列表、待处理等仍可留在助手模块内）；二级栏渲染助手列表，内容为现有子路由。
- `SettingsPage`：去掉 `PageShell` 与水平 Tabs；按 `section` 渲染右侧；由 `AppShell` 提供二级栏。
- `TemplatesPage`：去掉 `PageShell`；挂到壳的内容区。
- `App.tsx`：三模块挂在同一壳路由树下。

### 5.3 明确不做

- 镜像二级栏（列表→详情式）。
- 设置表单字段 / 后端 API 重构。
- 把 advanced「浏览器」并入一级轨（除非后续单独需求）。
- 视觉品牌重做；沿用 Semi 与现有助手壳的密度与颜色。

## 6. 迁移与兼容

- 从助手侧栏底部点「设置 / 镜像」的旧入口改为一级轨切换（或等价导航到对应顶级路径）。
- 书签 `/settings`、`/registry` 仍有效；`/settings` 无 section 时落到默认段落。
- `PageShell` 的 `NAV` 中与一级轨重复的项由壳配置接管，避免两套导航源。

## 7. 验收清单

- [ ] 一级轨可折叠为图标轨，刷新后状态保持。
- [ ] 助手：一级 + 列表 + 对话/详情仍可用（含移动端菜单）。
- [ ] 设置：二级含 Runtime、Git；admin 另有原 Tab 段落；无水平 Tabs。
- [ ] 非 admin 看不到 admin 段落与镜像一级入口。
- [ ] 镜像：一级选中后直接出列表页，无二级栏。
- [ ] Admin Save / Reload 仍作用于系统设置表单；Runtime / Git 独立保存。
- [ ] `/browser` 等 advanced 路由未被误删。

## 8. 测试建议

- 组件 / 路由：默认 section 解析、无权限 section 重定向、admin 过滤一级「镜像」。
- 手动：折叠一级轨、助手↔设置↔镜像切换、设置段落切换与脏表单保留、非 admin 设置仅两段。
