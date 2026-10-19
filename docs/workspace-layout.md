# 账号工作区目录规范（Canonical Layout）

`/workspace` 是账号级资料根：所有助手共享同一棵目录树，用户也能在「文件」页浏览与上传。
没有约定时，Agent 容易把报告、clone、临时文件直接堆在根目录，难以查找也难清理。

本规范约定顶层目录用途，并由控制面在 `EnsureAgent` 时播种空目录与 `AGENTS.md`。
技能、git 凭据、容器环境变量仍放在 `$HOME/.roundpen/`，**绝不**进 `/workspace`（见 [skills.md](skills.md)、[git-credentials.md](git-credentials.md)）。

## 目录树

```
/workspace/
├── AGENTS.md          # 给 Agent 读的布局合同（缺则播种；已有不覆盖）
├── uploads/           # 用户投放的原始资料（上传、导出、PDF 等）
├── projects/<name>/   # 每个项目 / git 仓库一个子目录
├── notes/             # 需要长期保留的笔记
├── outputs/           # Agent 产出的报告、计划、导出、截图
└── tmp/               # 临时与草稿；用完可删
```

| 路径 | 用途 | 示例 |
|------|------|------|
| `uploads/` | 用户提供的输入材料 | `uploads/brief.pdf` |
| `projects/<name>/` | 代码仓库与多文件工程；产品「项目」默认绑定此路径 | `projects/pokemon/`（本地 git 或之后再 clone） |
| `projects/<name>/.roundpen/context/` | 项目 Shared Context（不进 git） | 协调者与后续 Agent 共用的笔记 / playbook |
| `notes/` | 持久笔记 | `notes/rss-harvest-playbook.md` |
| `outputs/` | Agent 交付物 | `outputs/ISS-7-plan.md`、`outputs/weekly-report.md` |
| `tmp/` | 临时文件 | `tmp/scrape.json` |

`<name>` 使用 `^[a-z0-9][a-z0-9_-]*$`（小写、数字、`-`、`_`）。

## Agent 必须遵守

1. **根目录保持干净**：不要在 `/workspace` 根下新建散落文件（`report.md`、`ip.txt`、`ISS-*.md` 等）。
2. **不要在根目录 clone / 脚手架**：一律 `projects/<name>/`。
3. **不要**把技能、git 凭据、SSH 密钥写进 `/workspace`（用 `$HOME/.roundpen/`）。
4. 交付物进 `outputs/`；不确定归属时先放 `tmp/`，再挪到正确位置。
5. 新开顶层目录前，优先用上表；确实不适合时再问用户。

System Agent 的 system prompt 会引用本约定；stdio Agent（Claude Code 等）以工作区根的 `AGENTS.md` 为准。

## 控制面行为

| 时机 | 行为 |
|------|------|
| `EnsureAgent` | 确保五个标准目录存在；若缺少 `AGENTS.md` 则写入默认合同 |
| 已有 `AGENTS.md` | **不覆盖**，允许用户定制 |
| 已有杂乱文件 | **不自动迁移**；新写入遵守规范即可 |

## 与「文件」页

「文件」页展示整棵 `/workspace`。用户上传时仍可选路径；Agent 应把用户未指定位置的新资料优先放进 `uploads/`，产出放进 `outputs/`。
