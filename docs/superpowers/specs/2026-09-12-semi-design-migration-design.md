# Web 后台 Semi Design 全量迁移设计

日期：2026-09-12  
状态：已对齐，待实现计划  
范围：`web/` 控制台 UI 从 Tailwind + DaisyUI 切换为 Semi Design；含 MCP 配置。不含后端 API 变更。

## 1. 目标与原则

将 Roundpen Web 后台一次性切换到 **Semi Design 官方组件**，去掉 DaisyUI / Tailwind 双栈与自建控件壳。

**原则**

- **官方优先**：按钮、表单、表格、导航、弹窗、聊天、树等优先使用 `@douyinfe/semi-ui` / `@douyinfe/semi-icons` 现成能力，不自建平行组件库。
- **一次换干净**：不做长期 Daisy + Semi 双栈；同一迁移交付内移除 DaisyUI、Tailwind 依赖及相关样式补丁。
- **领域内核可保留**：xterm、noVNC 等无 Semi 对等物的渲染内核保留，仅用 Semi 容器与工具条包装。
- **实现期以 Semi MCP / 官方文档选型**：项目已配置 `.cursor/mcp.json` 的 `semi-mcp`。

## 2. 产品决策摘要

| 主题 | 决策 |
|------|------|
| 迁移策略 | 大爆炸全量切换（非双栈过渡） |
| 组件来源 | Semi Design 官方；非项目内已有自建 UI |
| 主题 | 明/暗可切换；默认深色 |
| 语言 | 默认 `zh_CN`；preference 预留 `locale`，本轮可不放语言开关 UI |
| 聊天 | `AIChatDialogue` + `AIChatInput` |
| 文件树 | Semi `Tree` |
| 终端 / VNC | 内核保留，Semi 壳包装 |
| 封装层 | 不建 `rp-ui` 业务组件库 |

## 3. 基础设施

### 3.1 依赖

**新增**

- `@douyinfe/semi-ui`
- `@douyinfe/semi-icons`
- 官方要求的样式入口（按当前 Semi 版本文档）

**移除**

- `daisyui`
- `tailwindcss` / `@tailwindcss/vite`
- `index.css` 中 Daisy 主题、`btn`/`input`/`modal` 等补丁与工具类依赖布局

**保留的极少量全局 CSS**

- `#root` 高度、`100dvh`、safe-area、`body.chat-lock` 等与键盘/滚动相关的行为补丁
- 不为视觉还原保留 Daisy 类名兼容层

### 3.2 应用根

- `main.tsx`（或等价根）包裹 `ConfigProvider`
- `locale` 默认 `zh_CN`，由 preference 驱动
- 主题 preference 同步 Semi 明暗（如 `body` 的 Semi dark class / 官方推荐方式）
- MCP：`.cursor/mcp.json` 注册 `semi-mcp`（`npx -y @douyinfe/semi-mcp`）

### 3.3 Preference

`localStorage` 键建议：`roundpen.ui`

```ts
type UiPreference = {
  theme: 'dark' | 'light'
  locale: 'zh_CN' | 'en'
}
```

- 默认：`{ theme: 'dark', locale: 'zh_CN' }`
- Header 提供明/暗切换控件（`Switch` 或图标 `Button`），立即生效并持久化
- 语言：数据结构与 `ConfigProvider` 接线到位；本轮可不展示语言开关

## 4. 布局与页面映射

### 4.1 壳

| 现有 | Semi |
|------|------|
| `PageShell` / `AssistantLayout` | `Layout`（Sider + Header + Content）+ `Nav` |
| 顶栏账号操作 | `Dropdown` / `Button` + `Modal`（改密） |
| 图标 | `@douyinfe/semi-icons`（品牌标可保留自有 SVG） |

### 4.2 标准页

| 区域 | Semi 组件 |
|------|-----------|
| Login | `Form`、`Input`、`Button`、`Typography`（可选 `Card`） |
| Settings / Templates / Sandboxes | `Form`、`Select`、`Table`、`Tabs`、`Modal`、`Popconfirm` |
| Assistant 列表 / 详情 / 创建 | `List` / `Descriptions`、`Input`、`TextArea`、`Tag`、`Button`、`Empty`、`Steps`（若向导保留） |
| 反馈 | `Spin`、`Banner`、`Toast` |

### 4.3 聊天

- 消息区 → `AIChatDialogue`；输入区 → `AIChatInput`
- **数据适配层**（非 UI 组件库）：将现有 `ChatLine` / ACP 消息（user、assistant、thought、tool_call、permission、event 等）映射为 Semi 消息结构；流式更新写入同一受控 `chats` 状态
- 工具调用、推理等优先用官方 content 类型与 `renderDialogueContentItem` 等扩展点
- 权限确认用 `Modal` / `Popconfirm`
- 删除自建聊天 UI：`ChatComposer`、`ToolCallCard`、自建气泡 Markdown 壳等（逻辑迁入适配层或官方渲染）

无法完美映射的类型：降级为文本（或官方支持的等价块），不阻塞会话主路径。

### 4.4 文件树

- `FileTree` → Semi `Tree`（目录懒加载、刷新按钮用官方 `Button` + icon）
- `files.list` API 不变，只转换节点数据形状

### 4.5 终端 / VNC

- xterm、noVNC 内核保留
- 外层：`Layout` / `Card` / `Tabs` / `Button` / `Spin`
- 去掉 Daisy 工具条与 pane 样式类

## 5. 清理与测试

### 5.1 清理清单

- `package.json` 移除 Daisy / Tailwind 相关依赖与 Vite Tailwind 插件
- 全库去掉 Daisy 类名与自定义 `chat-*` / `rp-*` 中仅服务旧主题的样式（行为必需的可改写为 Semi 布局或极少量全局 CSS）
- e2e：选择器迁移到稳定 `data-testid` 或 Semi 可访问角色；避免依赖 Daisy class

### 5.2 验收标准

1. 无 DaisyUI / Tailwind 运行时依赖；控件来自 Semi（终端/VNC 内核除外）
2. 明/暗可切换，刷新后保持
3. 默认中文（Semi `zh_CN` Locale）
4. 聊天为 `AIChatDialogue` + `AIChatInput`；文件树为 `Tree`；终端/VNC 有 Semi 壳
5. `web` 的 `build` 与核心 e2e（登录、助手、发消息）通过

## 6. 风险

| 风险 | 对策 |
|------|------|
| 聊天消息协议与 Semi AI 格式不一致 | 集中适配层；未知类型降级为文本 |
| e2e 大面积失效 | 迁移后优先修 Playwright；关键路径必须绿 |
| 包体积增大 | 接受 Semi 全量成本；本轮不做微前端拆分 |
| 本会话 Semi MCP 工具未加载 | 配置已落地；实现时启用 MCP 并以官方文档为准 |

## 7. 非目标

- 不改后端 API / ACP 协议本身（仅前端映射）
- 不强制本轮交付语言切换 UI
- 不自建设计系统封装层
- 不为视觉「完全复刻」旧 Daisy 主题而覆盖 Semi 大量 CSS
