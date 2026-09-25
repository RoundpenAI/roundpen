# Skills Reference

Roundpen 的技能（Skill）是一段可复用工作流指令：模型按名称调用技能，得到指令文本后
用普通的 `Read` / `Glob` / `Grep` / `Bash` 等工具执行，技能本身不携带运行时、不新增能力。

安装技能请一律走 **Skill 工具**（`install` 动作），不要用 `Write` 手工写文件 ——
存储位置、命名和格式由控制面统一管理，手工写入容易违反下面的约定。

## 目录规范（Canonical Layout）

| 项 | 约定 |
|----|------|
| 位置 | agent 容器内 `~/.roundpen/skills/<name>.md`（`$HOME` 下的 `.roundpen/skills`） |
| 结构 | 一个技能 = **一个文件** `<name>.md`（不是目录） |
| **不在** `/workspace` | 技能属于用户私有配置，**绝不放进** 项目工作区（`/workspace`），不参与 `git`、`Glob`、`Read` 等项目文件操作 |
| 命名 | `^[a-z0-9][a-z0-9_-]*$`（小写字母、数字、`-`、`_`），例如 `review-pr` |
| 内置技能 | `commit`、`review`、`fix`、`summarize`（不可删除，安装同名技能会遮蔽它并在覆盖前询问） |
| 生命周期 | 技能存在 agent 容器 HOME。容器重建后需重新安装；这是设计取舍：技能不进用户项目目录 |

## 文件格式（SKILL.md）

`---` 包裹的 frontmatter + 正文指令：

```markdown
---
name: review-pr
description: |
  审查最新一次 PR 变更，给出 bug / 安全 / 可读性结论，
  并列出带 file:line 的优先修复项。
args: true
allowedTools:
- Bash
- Read
- Glob
---
按下面的流程审查：
1. 用 `git diff HEAD~1` 或 `git diff origin/main...` 定位变更范围。
2. 逐个读变更文件，重点找 bug、安全问题和残缺处理。
3. 输出带 file:line 的结论；审查期间不要改文件。
```

frontmatter 字段：

| 字段 | 必填 | 说明 |
|------|------|------|
| `name` | 安装时非必填 | 安装时可用 `http(s)` 的 URL basename 推导；与请求中的 `skill` 参数冲突会报错 |
| `description` | 是 | 单行直接写；**多行用 `|` 字面块（保留换行）或 `>` 折叠块（合并成一行）** |
| `args` | 否 | `true` 表示技能接受模型传入的 `args` 自由文本 |
| `allowedTools` | 否 | 建议使用的工具列表（仅提示，不强约束） |

正文为空会安装失败；`description` 为空也会失败。

## 用户直接调用（Slash 命令）

技能不只对模型开放：用户在聊天输入框打 `/` 会弹出命令菜单，选中内置或已安装技能后发送，
控制面会把**同一份指令文本**注入为一条用户消息（与模型调 `Skill(action=invoke)` 完全一致，
包括 `args` 与 allowedTools 提示）。气泡里显示的是用户打的 `/review 参数` 原文，模型看到的是
展开后的指令。

同一入口还有两个动作命令：`/clear`（清空本会话上下文，聊天记录保留，模型从零开始）与
`/help`（列出可用命令）。已安装技能只在 agent 环境运行中才会出现在菜单里——列技能不会
拉起容器；`claude` 这类 stdio provider 的会话看不到已安装技能（技能目录属于 agent 容器的
`$HOME`，且 Skill 工具只在 System Agent 中注册）。

## Skill 工具动作

- `list`：查看全部技能（内置 + 已安装）。
- `install`：从 `url`（http/https，SSRF 防护）或 `content` 安装。覆盖已安装技能、遮蔽内置技能前都会询问用户。
- `invoke`（默认）：按名运行技能，返回指令文本。
- `remove`：删除已安装技能；内置技能不可删。
- 每轮（reply）最多调用 3 次，超出后让用户新开消息继续。

## 安装示例

```
Skill(action=install, url=https://example.com/review-pr.md)
Skill(action=install, content="---\nname: staticcheck\n...")
Skill(action=list)     # 确认装上
Skill(skill=review-pr) # 执行
```

## Agent 安装技能时的约定（避免困惑）

1. 先看本 Reference；按上面格式生成 SKILL.md 内容（含多行 `description` 用 `|` 块）。
2. 用 `Skill(action=install, ...)`，交控制面落盘到 `~/.roundpen/skills/`，**自己不要**往 `$HOME` 或 `/workspace` 写技能文件。
3. 命名用 `^[a-z0-9][a-z0-9_-]*$`；描述为空或多行未用块语法都会被拒绝。
4. 装完 `list` 确认；每轮 invoke 上限 3 次。